package model

import "time"

type SourceType string

const (
	SourceTypeKeyword      SourceType = "keyword"
	SourceTypePhrase       SourceType = "phrase"
	SourceTypeHashtag      SourceType = "hashtag"
	SourceTypeCashtag      SourceType = "cashtag"
	SourceTypeMention      SourceType = "mention"
	SourceTypeAccount      SourceType = "account"
	SourceTypeToAccount    SourceType = "to_account"
	SourceTypeRetweetsOf   SourceType = "retweets_of"
	SourceTypeURL          SourceType = "url"
	SourceTypeURLContains  SourceType = "url_contains"
	SourceTypeContext      SourceType = "context"
	SourceTypeEntity       SourceType = "entity"
	SourceTypeConversation SourceType = "conversation_id"
	SourceTypePlace        SourceType = "place"
	SourceTypePlaceCountry SourceType = "place_country"
	SourceTypePointRadius  SourceType = "point_radius"
	SourceTypeBoundingBox  SourceType = "bounding_box"
	SourceTypeLang         SourceType = "lang"
	SourceTypeIs           SourceType = "is"
	SourceTypeHas          SourceType = "has"
	SourceTypeFollowers    SourceType = "followers_count"
	SourceTypeTweets       SourceType = "tweets_count"
	SourceTypeFollowing    SourceType = "following_count"
	SourceTypeListed       SourceType = "listed_count"
	SourceTypeSample       SourceType = "sample"
)

type SourceClass string

const (
	SourceClassAtom     SourceClass = "atom"
	SourceClassModifier SourceClass = "modifier"
)

// SourceTypeInfo describes the canonical X API operator and class for a SourceType.
type SourceTypeInfo struct {
	Operator string
	Class    SourceClass
}

// SourceTypeMap is the canonical mapping from SourceType to operator prefix and class.
var SourceTypeMap = map[SourceType]SourceTypeInfo{
	SourceTypeKeyword:      {Operator: "", Class: SourceClassAtom},
	SourceTypePhrase:       {Operator: "", Class: SourceClassAtom},
	SourceTypeHashtag:      {Operator: "", Class: SourceClassAtom},
	SourceTypeCashtag:      {Operator: "", Class: SourceClassAtom},
	SourceTypeMention:      {Operator: "", Class: SourceClassAtom},
	SourceTypeAccount:      {Operator: "from:", Class: SourceClassAtom},
	SourceTypeToAccount:    {Operator: "to:", Class: SourceClassAtom},
	SourceTypeRetweetsOf:   {Operator: "retweets_of:", Class: SourceClassAtom},
	SourceTypeURL:          {Operator: "url:", Class: SourceClassAtom},
	SourceTypeURLContains:  {Operator: "url_contains:", Class: SourceClassAtom},
	SourceTypeContext:      {Operator: "context:", Class: SourceClassAtom},
	SourceTypeEntity:       {Operator: "entity:", Class: SourceClassAtom},
	SourceTypeConversation: {Operator: "conversation_id:", Class: SourceClassAtom},
	SourceTypePlace:        {Operator: "place:", Class: SourceClassAtom},
	SourceTypePlaceCountry: {Operator: "place_country:", Class: SourceClassAtom},
	SourceTypePointRadius:  {Operator: "point_radius:", Class: SourceClassAtom},
	SourceTypeBoundingBox:  {Operator: "bounding_box:", Class: SourceClassAtom},
	SourceTypeLang:         {Operator: "lang:", Class: SourceClassModifier},
	SourceTypeIs:           {Operator: "is:", Class: SourceClassModifier},
	SourceTypeHas:          {Operator: "has:", Class: SourceClassModifier},
	SourceTypeFollowers:    {Operator: "followers_count:", Class: SourceClassModifier},
	SourceTypeTweets:       {Operator: "tweets_count:", Class: SourceClassModifier},
	SourceTypeFollowing:    {Operator: "following_count:", Class: SourceClassModifier},
	SourceTypeListed:       {Operator: "listed_count:", Class: SourceClassModifier},
	SourceTypeSample:       {Operator: "sample:", Class: SourceClassModifier},
}

type Source struct {
	ID        int64       `json:"id"`
	Name      string      `json:"name"`
	Type      SourceType  `json:"type"`
	Class     SourceClass `json:"class"`
	Operator  string      `json:"operator"`
	Value     string      `json:"value"`
	Active    bool        `json:"active"`
	CreatedAt time.Time   `json:"created_at"`
	UpdatedAt time.Time   `json:"updated_at"`
}

// Render returns the X API syntax for this source (operator + value).
func (s *Source) Render() string {
	if s.Operator == "" {
		return s.Value
	}
	return s.Operator + s.Value
}
