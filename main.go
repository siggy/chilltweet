package main

import (
	"net/url"
	"os"
	"sort"
	"strconv"

	"github.com/ChimeraCoder/anaconda"
	log "github.com/sirupsen/logrus"
)

var CONSUMER_KEY = os.Getenv("TWITTER_CONSUMER_KEY")
var CONSUMER_SECRET = os.Getenv("TWITTER_CONSUMER_SECRET")
var ACCESS_TOKEN = os.Getenv("TWITTER_OAUTH_TOKEN")
var ACCESS_TOKEN_SECRET = os.Getenv("TWITTER_OAUTH_TOKEN_SECRET")

type userCount struct {
	user   string
	tweets int
}

type userCounts []userCount

func (u userCounts) Len() int           { return len(u) }
func (u userCounts) Swap(i, j int)      { u[i], u[j] = u[j], u[i] }
func (u userCounts) Less(i, j int) bool { return u[i].tweets > u[j].tweets }

func main() {
	if len(os.Args) != 2 {
		log.Errorf("usage: chilltweet screen_name")
		return
	}

	sourceUser := os.Args[1]

	if CONSUMER_KEY == "" || CONSUMER_SECRET == "" || ACCESS_TOKEN == "" || ACCESS_TOKEN_SECRET == "" {
		log.Errorf("env vars not set")
		return
	}

	anaconda.SetConsumerKey(CONSUMER_KEY)
	anaconda.SetConsumerSecret(CONSUMER_SECRET)
	api := anaconda.NewTwitterApi(ACCESS_TOKEN, ACCESS_TOKEN_SECRET)
	defer api.Close()

	c, err := api.GetFriendsIds(
		url.Values{"screen_name": []string{sourceUser}},
	)
	if err != nil {
		log.Errorf("GetFriendsIds failed with: %+v", err)
		return
	}
	log.Infof("Got %+v follows", len(c.Ids))

	v := url.Values{
		"count":           []string{"200"},
		"exclude_replies": []string{"true"},
	}

	// screenName => []tweets
	userTweets := make(map[string][]anaconda.Tweet)

	// this keeps track of the highest velocity user's 3200th newest tweet.
	// all other user timelines are truncated beyond this tweet, to ensure we
	// measure the same time range across users.
	highestLastID := int64(0)

	for _, userID := range c.Ids {
		v.Del("max_id")
		v.Set("user_id", strconv.FormatInt(userID, 10))
		var screenName string
		var lastID int64
		for {
			tweets, err := api.GetUserTimeline(v)
			if err != nil {
				log.Errorf("GetUserTimeline failed with: %+v", err)
				return
			}

			if len(tweets) == 0 {
				break
			}

			screenName = tweets[0].User.ScreenName

			lastID = tweets[len(tweets)-1].Id
			v.Set(
				"max_id",
				strconv.FormatInt(lastID-1, 10),
			)

			userTweets[screenName] = append(userTweets[screenName], tweets...)
			log.Debugf("%+v: %+v tweets | max_id: %+v", screenName, len(userTweets[screenName]), v.Get("max_id"))
			if lastID < highestLastID {
				// we're prior to the oldest tweet of the highest velocity user, bail
				break
			}
		}

		if lastID > highestLastID {
			highestLastID = lastID
		}

		log.Infof("%+v: %+v tweets | lastID: %+v | highestLastID: %+v",
			screenName,
			len(userTweets[screenName]),
			lastID,
			highestLastID,
		)
	}

	log.Infof("got %+v users' tweets", len(userTweets))

	// count tweets per user
	counts := userCounts{}
	for user, tweets := range userTweets {
		count := 0
		// this assumes tweets came in ordered newest => oldest
		for _, tweet := range tweets {
			if tweet.Id < highestLastID {
				break
			}
			count++
		}
		counts = append(counts, userCount{user: user, tweets: count})
	}

	sort.Sort(counts)

	log.Infof("COUNTS %+v", counts)
}
