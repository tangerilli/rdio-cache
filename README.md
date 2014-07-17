rdio-cache
==========

Updates your Rdio offline sync list to include the tracks you listen to most often. The goal of this is to decrease data usage on your mobile device, by increasing the chance that the music you're listening to is cached locally on the device.  If you generally listen to Rdio stations, this may not work very well, but if you tend to listen to music in your collection, it will.

Usage
-----

No binaries are provided, so you need a working go environment (see http://golang.org/doc/install). Once go is setup, run 'go get github.com/tangerilli/rdio-cache'.  Then run 'go build github.com/tangerilli/rdio-cache'.

If you run the resulting binary, it will prompt you to visit an rdio.com URL and authorize the app. Once you've done this, you can enter the provided PIN.  The application will save this to a configuration file in the current directory, and then start running.  By default, it will fetch up to 1000 songs from your listening history, rank them, and then update your sync list to consist of the 100 highest ranked songs (WARNING: your existing sync list will be wiped out).

The current ranking algorithm works by looking at both the number of times a song was played, and how recently it was played.  Songs played more recently are given a larger weight than songs played awhile ago.

Configuration
-------------

The application looks for a 'config.json' file in the current directory.  The syntax is:

    {
        "ConsumerKey": "",
        "ConsumerSecret": "",
        "Preferences": {
            "MaxHistory": 1000,
            "MaxAge": 21,
            "MaxSync": 100
        }
    }

The ConsumerKey and ConsumerSecret are for the Rdio API.  By default, the application will use defaults, if these are not provided. MaxHistory sets the maximum number of songs to fetch from your listening history at any one time. MaxAge is a value in days. Songs older than this are not considered when doing the ranking. MaxSync is the number of songs to include in the sync list (so modify it based on how much space you want to use on your mobile devices).
