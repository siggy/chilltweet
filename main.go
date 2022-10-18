package main

import (
	"errors"
	"net/http"
	"os"
	"sort"
	"time"

	"github.com/dghubble/go-twitter/twitter"
	"github.com/dghubble/oauth1"
	log "github.com/sirupsen/logrus"
)

type logFormatter struct{}

func (f *logFormatter) Format(entry *log.Entry) ([]byte, error) {
	return []byte(entry.Message + "\n"), nil
}

type userCount struct {
	user   string
	tweets int
}

type userCounts []userCount

func (u userCounts) Len() int           { return len(u) }
func (u userCounts) Swap(i, j int)      { u[i], u[j] = u[j], u[i] }
func (u userCounts) Less(i, j int) bool { return u[i].tweets > u[j].tweets }

func main() {
	log.SetLevel(log.DebugLevel)

	if len(os.Args) != 2 {
		log.Errorf("usage: chilltweet screen_name")
		return
	}

	log.SetFormatter(new(logFormatter))

	sourceScreenName := os.Args[1]

	consumerKey := os.Getenv("TWITTER_CONSUMER_KEY")
	consumerSecret := os.Getenv("TWITTER_CONSUMER_SECRET")
	accessToken := os.Getenv("TWITTER_OAUTH_TOKEN")
	accessTokenSecret := os.Getenv("TWITTER_OAUTH_TOKEN_SECRET")

	config := oauth1.NewConfig(consumerKey, consumerSecret)
	token := oauth1.NewToken(accessToken, accessTokenSecret)
	httpClient := config.Client(oauth1.NoContext, token)

	client := twitter.NewClient(httpClient)

	friends, resp, err := client.Friends.IDs(
		&twitter.FriendIDParams{
			ScreenName: sourceScreenName,
			Count:      5000,
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("failed to get friends: %+v", resp)
	}

	log.Infof("Got %d friends for %s", len(friends.IDs), sourceScreenName)

	// screenName => []tweets
	userTweets := make(map[string][]twitter.Tweet)

	// this keeps track of the highest velocity user's 3200th newest tweet.
	// all other user timelines are truncated beyond this tweet, to ensure we
	// measure the same time range across users.
	highestLastTweet := twitter.Tweet{}
	excludeReplies := true
	includeRetweets := true

	for i, userID := range friends.IDs {
		maxID := int64(0)
		var screenName string

		for {
			tweets, resp, err := client.Timelines.UserTimeline(
				&twitter.UserTimelineParams{
					UserID:          userID,
					Count:           200,
					MaxID:           maxID,
					ExcludeReplies:  &excludeReplies,
					IncludeRetweets: &includeRetweets,
				},
			)
			if err != nil {
				apiError := twitter.APIError{}
				if errors.As(err, &apiError) {
					switch apiError.Errors[0].Code {
					case 88:
						log.Warnf("got APIError[%s] %+v, sleeping for 15 minutes from %s ...", apiError, resp, time.Now())
						time.Sleep(15 * time.Minute)
						continue
					case 131:
						log.Warnf("got APIError[%s]: %+v, sleeping for 10 seconds ...", apiError, resp)
						time.Sleep(10 * time.Second)
						continue
					}
				}

				log.Errorf("UserTimeline failed with: %s", err)
				return
			}
			if resp.StatusCode != http.StatusOK {
				log.Errorf("failed to get UserTimeline: %+v", resp)
				return
			}
			if len(tweets) == 0 || (len(tweets) == 1 && tweets[0].ID == maxID) {
				break
			}

			screenName = tweets[0].User.ScreenName

			maxID = tweets[len(tweets)-1].ID

			userTweets[screenName] = append(userTweets[screenName], tweets...)
			log.Debugf("  %-15v: %+4v tweets | max_id: %+19v", screenName, len(userTweets[screenName]), maxID)
			if maxID < highestLastTweet.ID {
				// we're earlier than the oldest tweet of the highest velocity user, bail
				break
			}
		}

		// only increase highestLastID if the user has more than 1000 tweets total
		if maxID > highestLastTweet.ID && len(userTweets[screenName]) > 1000 {
			highestLastTweet = userTweets[screenName][len(userTweets[screenName])-1]
		}

		log.Infof("%4d / %4d | %-15v: %+4v | maxID: %+19v | highestLastTweet[%s]: https://twitter.com/%s/status/%+19v",
			i+1, len(friends.IDs),
			screenName,
			len(userTweets[screenName]),
			maxID,
			highestLastTweet.CreatedAt,
			highestLastTweet.User.ScreenName,
			highestLastTweet.ID,
		)
	}

	log.Infof("got %+v users' tweets", len(userTweets))

	// count tweets per user
	totalTweets := 0
	counts := userCounts{}
	for user, tweets := range userTweets {
		count := 0
		// this assumes tweets came in ordered newest => oldest
		for _, tweet := range tweets {
			if tweet.ID < highestLastTweet.ID {
				break
			}
			count++
		}
		counts = append(counts, userCount{user: user, tweets: count})
		totalTweets += count
	}

	sort.Sort(counts)

	for _, userCount := range counts {
		percent := 100.0 * float64(userCount.tweets) / float64(totalTweets)
		log.Infof("%5.2f%% %+4v: %+v", percent, userCount.tweets, userCount.user)
	}
}
