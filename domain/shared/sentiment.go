package shared

type Sentiment string

const (
	SentimentPositive Sentiment = "positive"
	SentimentNeutral  Sentiment = "neutral"
	SentimentNegative Sentiment = "negative"
)

func (s Sentiment) Valid() bool {
	switch s {
	case SentimentPositive, SentimentNeutral, SentimentNegative:
		return true
	}
	return false
}

func SentimentValues() []string {
	return []string{string(SentimentPositive), string(SentimentNeutral), string(SentimentNegative)}
}
