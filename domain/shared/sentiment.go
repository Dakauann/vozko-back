package shared

// Sentiment is the emotional tone a model reads in a customer's words.
//
// It lives here rather than in one channel's analysis package because it is
// not conversation-specific: a WhatsApp transcript, a call and a public
// Instagram comment all carry the same three values, and storing the SAME
// values is what lets a cross-channel sentiment view exist at all.
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

// SentimentValues returns the allowed values, in display order.
func SentimentValues() []string {
	return []string{string(SentimentPositive), string(SentimentNeutral), string(SentimentNegative)}
}
