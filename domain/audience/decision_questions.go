package audience

import (
	"fmt"
	"math"

	"vozko/domain/decision"
	"vozko/domain/shared"
)

const LanguageUndetermined = "und"

var languageOptions = []decision.Option{
	{Key: "pt", Description: "português"},
	{Key: "es", Description: "espanhol"},
	{Key: "en", Description: "inglês"},
	{Key: LanguageUndetermined, Description: "outro idioma, ou não dá para saber"},
}

var qualityLevelMeaning = map[shared.QualityLevel]string{
	shared.QualityLevelNone:   "ausente: a dimensão foi observada e não aconteceu",
	shared.QualityLevelLow:    "fraco",
	shared.QualityLevelMedium: "razoável",
	shared.QualityLevelHigh:   "forte ou excelente",
}

func choiceFromField(field shared.ClassificationField) decision.Question {
	options := make([]decision.Option, 0, len(field.Options))
	for _, option := range field.Options {
		options = append(options, decision.Option{Key: option.Value, Description: option.Description})
	}
	return decision.Choice(field.Title+": "+field.Intro, options...)
}

func scoreFromDimension(dimension shared.QualityDimension) decision.Question {
	levels := make([]string, 0, len(shared.QualityLevelValues()))
	for _, value := range shared.QualityLevelValues() {
		levels = append(levels, value+": "+qualityLevelMeaning[shared.QualityLevel(value)])
	}
	return decision.Score(dimension.Label+": "+dimension.Description, levels...)
}

func languageQuestion() decision.Question {
	return decision.Choice("Idioma predominante das mensagens do cliente.", languageOptions...)
}

func ConversationDecisionQuestions() map[string]decision.Question {
	questions := map[string]decision.Question{FieldLanguage: languageQuestion()}
	for _, field := range ConversationClassificationFields() {
		questions[field.Key] = choiceFromField(field)
	}
	for _, dimension := range ConversationQualityDimensions() {
		questions[dimension.Key] = scoreFromDimension(dimension)
	}
	return questions
}

func CommentDecisionQuestions(topics TopicSet) map[string]decision.Question {
	questions := map[string]decision.Question{
		FieldLanguage: languageQuestion(),
		FieldIsSpam: decision.YesNo("O comentário é spam?",
			"propaganda de terceiros, golpe, link suspeito, corrente, bot ou texto sem relação",
			"comentário genuíno, mesmo que crítico ou fora de tom"),
	}
	for _, field := range ClassificationFields() {
		questions[field.Key] = choiceFromField(field)
	}
	for _, dimension := range SeverityDimensions() {
		questions[dimension.Key] = scoreFromDimension(dimension)
	}
	if len(topics) >= 2 {
		options := make([]decision.Option, 0, len(topics))
		for _, topic := range topics {
			description := topic.Label
			if topic.Description != "" {
				description += ": " + topic.Description
			}
			options = append(options, decision.Option{Key: topic.Key, Description: description})
		}
		questions[FieldTopicKey] = decision.Choice("Qual tema da lista o comentário aborda?", options...)
	}
	return questions
}

type ConversationReading struct {
	Classification Classification
	Certainty      map[string]float64
}

func (r ConversationReading) Uncertain(field string, min decision.Threshold) bool {
	certainty, ok := r.Certainty[field]
	return !ok || certainty < float64(min)
}

func ConversationReadingFrom(result decision.Result) (ConversationReading, error) {
	answers := answerSet{result: result, certainty: map[string]float64{}}
	quality := make(map[string]shared.QualityLevel, len(ConversationQualityDimensions()))
	for _, dimension := range ConversationQualityDimensions() {
		quality[dimension.Key] = answers.level(dimension.Key)
	}
	c := Classification{
		Interest:      Interest(answers.choice(FieldInterest)),
		Disposition:   Disposition(answers.choice(FieldDisposition)),
		Sentiment:     shared.Sentiment(answers.choice(FieldSentiment)),
		Qualification: Qualification(answers.choice(FieldQualification)),
		NextAction:    NextAction(answers.choice(FieldNextAction)),
		Language:      answers.choice(FieldLanguage),
		Quality:       NewConversationQuality(quality),
	}
	if answers.err != nil {
		return ConversationReading{}, answers.err
	}
	if err := c.ValidateConversation(); err != nil {
		return ConversationReading{}, err
	}
	return ConversationReading{Classification: c, Certainty: answers.certainty}, nil
}

func CommentClassificationFrom(result decision.Result, topics TopicSet) (Classification, error) {
	answers := answerSet{result: result, certainty: map[string]float64{}}
	c := Classification{
		Sentiment:      shared.Sentiment(answers.choice(FieldSentiment)),
		Stance:         Stance(answers.choice(FieldStance)),
		Intent:         Intent(answers.choice(FieldIntent)),
		IsSpam:         answers.yes(FieldIsSpam),
		Language:       answers.choice(FieldLanguage),
		Toxicity:       answers.level(SeverityKeyToxicity),
		PersonalAttack: answers.level(SeverityKeyPersonalAttack),
		LegalRisk:      answers.level(SeverityKeyLegalRisk),
		TopicKey:       TopicKeyOther,
	}
	if len(topics) >= 2 {
		c.TopicKey = answers.choice(FieldTopicKey)
	}
	if answers.err != nil {
		return Classification{}, answers.err
	}
	if err := c.Validate(topics); err != nil {
		return Classification{}, err
	}
	return c, nil
}

type answerSet struct {
	result    decision.Result
	certainty map[string]float64
	err       error
}

func (s *answerSet) answer(id string, kind decision.Kind) (decision.Answer, bool) {
	answer, ok := s.result.Answer(id)
	if !ok || answer.Kind != kind {
		if s.err == nil {
			s.err = fmt.Errorf("%w: no %s answer for %q", ErrInvalidClassification, kind, id)
		}
		return decision.Answer{}, false
	}
	s.certainty[id] = answer.Certainty()
	return answer, true
}

func (s *answerSet) choice(id string) string {
	answer, _ := s.answer(id, decision.KindChoice)
	return answer.Choice
}

func (s *answerSet) yes(id string) bool {
	answer, _ := s.answer(id, decision.KindYesNo)
	return answer.Yes >= 0.5
}

func (s *answerSet) level(id string) shared.QualityLevel {
	answer, ok := s.answer(id, decision.KindScore)
	if !ok {
		return ""
	}
	levels := shared.QualityLevelValues()
	index := int(math.Round(answer.Score))
	if index < 0 || index >= len(levels) {
		if s.err == nil {
			s.err = fmt.Errorf("%w: %q scored %v outside the levels", ErrInvalidClassification, id, answer.Score)
		}
		return ""
	}
	return shared.QualityLevel(levels[index])
}

const FieldRole = "role"

var roleOptions = []decision.Option{
	{Key: string(RoleUnknown), Description: "público comum, ou os comentários não dizem claramente o que a pessoa faz"},
	{Key: string(RolePolitician), Description: "político ou ocupante de cargo eletivo, segundo o que a própria pessoa diz"},
	{Key: string(RoleJournalist), Description: "jornalista, repórter ou comunicador que fala do próprio trabalho"},
	{Key: string(RolePublicServant), Description: "servidor público que fala do próprio cargo ou órgão"},
	{Key: string(RoleBusinessOwner), Description: "dono ou dona de negócio que fala da própria empresa"},
	{Key: string(RoleProfessional), Description: "profissional que fala do próprio ofício, como médico, advogado ou professor"},
	{Key: string(RoleActivist), Description: "ativista ou militante que se apresenta por uma causa"},
}

func AuthorRoleQuestions() map[string]decision.Question {
	return map[string]decision.Question{
		FieldRole: decision.Choice("Qual o papel público de quem escreveu estes comentários? Baseie-se só no que a pessoa diz sobre si ou sobre o trabalho dela, nunca no tom ou na opinião.", roleOptions...),
	}
}

func AuthorRoleFrom(result decision.Result) (AuthorRole, shared.QualityLevel, error) {
	answers := answerSet{result: result, certainty: map[string]float64{}}
	choice := answers.choice(FieldRole)
	if answers.err != nil {
		return "", "", answers.err
	}
	role, ok := ParseAuthorRole(choice)
	if !ok {
		return "", "", fmt.Errorf("%w: role %q", ErrInvalidClassification, choice)
	}
	return role, ConfidenceLevel(answers.certainty[FieldRole]), nil
}

func ConfidenceLevel(certainty float64) shared.QualityLevel {
	switch {
	case certainty >= 0.85:
		return shared.QualityLevelHigh
	case certainty >= 0.7:
		return shared.QualityLevelMedium
	case certainty >= 0.5:
		return shared.QualityLevelLow
	}
	return shared.QualityLevelNone
}
