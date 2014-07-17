package main

import (
	"bufio"
	"encoding/json"
	"github.com/grantmd/go-rdio"
	"log"
	"os"
	"sort"
	"time"
)

type Config struct {
	ConsumerKey    string
	ConsumerSecret string
	Token          string
	TokenSecret    string
	Preferences    struct {
		MaxHistory int // maximum number of tracks to cache
		MaxAge     int // maximum age of a track to consider (in days)
		MaxSync    int // maximum number of songs to sync to mobile
	}

	saveKeys bool
}

type RankedTrack struct {
	historyTrack rdio.HistoryTrack
	rank         float64
}
type ByRank []*RankedTrack

func (a ByRank) Len() int           { return len(a) }
func (a ByRank) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByRank) Less(i, j int) bool { return a[i].rank > a[j].rank }

func loadConfig(path string) (*Config, error) {
	var config Config
	config.Preferences.MaxHistory = 1000
	config.Preferences.MaxAge = 21
	config.Preferences.MaxSync = 100

	cfgFile, err := os.Open(path)
	if err != nil {
		return &config, nil
	}
	defer cfgFile.Close()

	decoder := json.NewDecoder(cfgFile)
	err = decoder.Decode(&config)
	if err != nil {
		return nil, err
	}
	return &config, nil
}

func writeConfig(path string, config *Config) error {
	cfgFile, err := os.Create(path)
	if err != nil {
		return err
	}
	defer cfgFile.Close()

	encoded, err := json.MarshalIndent(config, "", "    ")
	if err != nil {
		return err
	}
	cfgFile.Write(encoded)
	return nil
}

func loadCache(path string) []rdio.HistorySource {
	sources := make([]rdio.HistorySource, 0)
	f, err := os.Open(path)
	if err != nil {
		return sources
	}
	defer f.Close()

	decoder := json.NewDecoder(f)
	// Don't care if there was an error, we'll just return the empty list anyways
	decoder.Decode(&sources)

	log.Printf("Loaded %d items from cache\n", len(sources))
	return sources
}

func writeCache(path string, sources []rdio.HistorySource) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	log.Printf("Writing %d items to cache\n", len(sources))
	encoded, err := json.MarshalIndent(sources, "", "    ")
	if err != nil {
		return err
	}
	f.Write(encoded)
	return nil
}

func parseHistoryTime(timeString string) (time.Time, error) {
	return time.Parse("2006-01-02T15:04:05", timeString)
}

func updateCache(c *rdio.Client, user *rdio.User, maxHistory int) []rdio.HistorySource {
	cache := loadCache("history.json")

	lastCachedTime := time.Unix(0, 0)
	if len(cache) > 0 {
		lastCachedTime, _ = parseHistoryTime(cache[0].Time)
	}

	pos := 0

loop:
	for {
		log.Printf("Requesting items %d-%d\n", pos, pos+9)
		sources, err := c.GetHistoryForUser(user.Key, pos, 10)
		if err != nil {
			log.Fatalf("Could not get sources list: %s\n", err.Error())
		}
		for _, source := range sources {
			timestamp, err := time.Parse("2006-01-02T15:04:05", source.Time)
			if err == nil && timestamp.Before(lastCachedTime) || timestamp.Equal(lastCachedTime) {
				log.Println("Already seen this data, finished updating the cache")
				break loop
			}
			log.Printf("Adding %s from %s\n", source.Source.Name, source.Time)
			cache = append(cache, source)
			pos += len(source.Tracks)
		}

		if pos > maxHistory {
			break
		}
	}

	writeCache("history.json", cache)
	return cache
}

// todo: write some tests for rankTracks
func rankTracks(sources []rdio.HistorySource, maxAge int) []*RankedTrack {
	tracks := make(map[string]*RankedTrack)
	for _, source := range sources {
		for _, track := range source.Tracks {
			trackTime, err := parseHistoryTime(track.Time)
			if err != nil {
				log.Printf("Error parsing track time %s\n", track.Time)
				continue
			}
			age := time.Now().Sub(trackTime)
			if (age.Hours() / 24) > float64(maxAge) {
				continue
			}

			rankedTrack, ok := tracks[track.Track.Key]
			if !ok {
				rankedTrack = &RankedTrack{track, 0}
				tracks[track.Track.Key] = rankedTrack
			}

			// add an additional weighting to the rank based on how long ago the song was played (more recently means
			// a higher ranking)
			weighting := 2.0 / (age.Hours() / 24.0)
			rankedTrack.rank += (1 + weighting)
		}
	}

	trackSlice := make([]*RankedTrack, 0, len(tracks))
	for _, t := range tracks {
		trackSlice = append(trackSlice, t)
	}
	return trackSlice
}

func updateSyncList(c *rdio.Client, tracks []*RankedTrack, maxSync int) {
	currentlySynced, err := c.GetOfflineTracks(0, 1000)
	log.Printf("%d synced tracks\n", len(currentlySynced))
	if err != nil {
		log.Fatalf("Could not get offline tracks: %s\n", err.Error())
	}

	syncedKeyMap := make(map[string]bool)
	syncedKeyList := make([]string, 0, maxSync)
	for i, track := range tracks {
		if i >= maxSync {
			break
		}

		log.Printf("Need to sync %s\n", track.historyTrack.Track.Name)
		syncedKeyMap[track.historyTrack.Track.Key] = true
		syncedKeyList = append(syncedKeyList, track.historyTrack.Track.Key)
	}
	log.Printf("Syncing %d tracks\n", len(syncedKeyList))
	c.SetAvailableOffline(syncedKeyList, true)

	unsync := make([]string, 0, len(currentlySynced))
	for _, track := range currentlySynced {
		_, shouldBeSynced := syncedKeyMap[track.Key]
		if !shouldBeSynced {
			log.Printf("Need to unsync %s\n", track.Name)
			unsync = append(unsync, track.Key)
		}
	}
	log.Printf("Need to unsync %d tracks\n", len(unsync))
	c.SetAvailableOffline(unsync, false)
}

func update(c *rdio.Client, config *Config) {
	/*
	   // Find the X most played songs for the user (ideally independent of their collection)
	       // Figure out the user key
	       // Fetch any history that we don't already have cached locally
	           // Fetch the first X history items
	           // Iterate through them, and if any timestamps match the cache stop, otherwise cache
	           // Repeat until MAX_HISTORY has been reached
	       // Delete any cached history older than X days
	       // Go through the cache, and assign a weight to each track. Each time a track is seen, the inverse of (now-play time)
	       // is added to its score
	   // Make those songs synced
	       // Sort the list of scored tracks
	       // Take the top X tracks as the sync list
	       // Mark all of those tracks as synced
	   // Unsync any songs not in the list
	       // Go through the list of tracks currently being synced
	       // Unsync anything not in the list above
	*/

	// Figure out the current user
	user, err := c.CurrentUser()
	if err != nil {
		log.Fatalf("Could not determine current user: %s\n", err.Error())
	}

	// Get the latest history
	history := updateCache(c, user, config.Preferences.MaxHistory)
	// Get a list of the tracks, with ranking metadata
	ranked := rankTracks(history, config.Preferences.MaxAge)
	// Sort the list
	sort.Sort(ByRank(ranked))
	for _, rankedTrack := range ranked {
		log.Printf("%s (score = %f)\n", rankedTrack.historyTrack.Track.Name, rankedTrack.rank)
	}

	// Update the list of tracks to sync with rdio
	updateSyncList(c, ranked, config.Preferences.MaxSync)
}

func updateToken(c *rdio.Client, config *Config) {
	auth, err := c.StartAuth()
	if err != nil {
		log.Fatalf("Error: %s\n", err.Error())
	}

	log.Printf("Authorize this application at: %s?oauth_token=%s\n", auth.Get("login_url"), auth.Get("oauth_token"))
	log.Print("Enter the PIN / OAuth verifier: ")
	bio := bufio.NewReader(os.Stdin)

	verifier, _, err := bio.ReadLine()
	if err != nil {
		log.Fatalf(err.Error())
	}
	log.Println()

	// Check their PIN and complete auth so we can make calls
	auth, err = c.CompleteAuth(string(verifier))
	if err != nil {
		log.Fatalf(err.Error())
	}

	c.Token = auth.Get("oauth_token")
	c.TokenSecret = auth.Get("oauth_token_secret")
	config.Token = c.Token
	config.TokenSecret = c.TokenSecret
	err = writeConfig("config.json", config)
	if err != nil {
		log.Printf("Could not write new configuration: %s\n", err.Error())
	}
}

func main() {
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatal("Error loading configuration: ", err.Error())
	}

	// Use a compiled in key and secret, unless they're specified in the config file
	consumerKey := config.ConsumerKey
	if consumerKey == "" {
		consumerKey = "USEYOUROWN"
	}
	consumerSecret := config.ConsumerSecret
	if consumerSecret == "" {
		consumerSecret = "USEYOUROWN"
	}

	log.Println("Connecting to rdio")

	c := &rdio.Client{
		ConsumerKey:    consumerKey,
		ConsumerSecret: consumerSecret,
		Token:          config.Token,
		TokenSecret:    config.TokenSecret,
	}

	// TODO: Deal with the case where the token expires
	if c.Token == "" {
		updateToken(c, config)
	}

	update(c, config)
}
