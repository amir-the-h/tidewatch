package model

import "time"

type XTermType string

const (
	XTermTypeKeyword      XTermType = "keyword"
	XTermTypePhrase       XTermType = "phrase"
	XTermTypeHashtag      XTermType = "hashtag"
	XTermTypeCashtag      XTermType = "cashtag"
	XTermTypeMention      XTermType = "mention"
	XTermTypeAccount      XTermType = "account"
	XTermTypeToAccount    XTermType = "to_account"
	XTermTypeRetweetsOf   XTermType = "retweets_of"
	XTermTypeURL          XTermType = "url"
	XTermTypeURLContains  XTermType = "url_contains"
	XTermTypeContext      XTermType = "context"
	XTermTypeEntity       XTermType = "entity"
	XTermTypeConversation XTermType = "conversation_id"
	XTermTypePlace        XTermType = "place"
	XTermTypePlaceCountry XTermType = "place_country"
	XTermTypePointRadius  XTermType = "point_radius"
	XTermTypeBoundingBox  XTermType = "bounding_box"
	XTermTypeLang         XTermType = "lang"
	XTermTypeIs           XTermType = "is"
	XTermTypeHas          XTermType = "has"
	XTermTypeFollowers    XTermType = "followers_count"
	XTermTypeTweets       XTermType = "tweets_count"
	XTermTypeFollowing    XTermType = "following_count"
	XTermTypeListed       XTermType = "listed_count"
	XTermTypeSample       XTermType = "sample"
)

type XTermClass string

const (
	XTermClassAtom     XTermClass = "atom"
	XTermClassModifier XTermClass = "modifier"
)

// XTermTypeInfo describes the canonical X API operator and class for a SourceType.
type XTermTypeInfo struct {
	Operator string
	Class    XTermClass
}

// XTermTypeMap is the canonical mapping from SourceType to operator prefix and class.
var XTermTypeMap = map[XTermType]XTermTypeInfo{
	XTermTypeKeyword:      {Operator: "", Class: XTermClassAtom},
	XTermTypePhrase:       {Operator: "", Class: XTermClassAtom},
	XTermTypeHashtag:      {Operator: "", Class: XTermClassAtom},
	XTermTypeCashtag:      {Operator: "", Class: XTermClassAtom},
	XTermTypeMention:      {Operator: "", Class: XTermClassAtom},
	XTermTypeAccount:      {Operator: "from:", Class: XTermClassAtom},
	XTermTypeToAccount:    {Operator: "to:", Class: XTermClassAtom},
	XTermTypeRetweetsOf:   {Operator: "retweets_of:", Class: XTermClassAtom},
	XTermTypeURL:          {Operator: "url:", Class: XTermClassAtom},
	XTermTypeURLContains:  {Operator: "url_contains:", Class: XTermClassAtom},
	XTermTypeContext:      {Operator: "context:", Class: XTermClassAtom},
	XTermTypeEntity:       {Operator: "entity:", Class: XTermClassAtom},
	XTermTypeConversation: {Operator: "conversation_id:", Class: XTermClassAtom},
	XTermTypePlace:        {Operator: "place:", Class: XTermClassAtom},
	XTermTypePlaceCountry: {Operator: "place_country:", Class: XTermClassAtom},
	XTermTypePointRadius:  {Operator: "point_radius:", Class: XTermClassAtom},
	XTermTypeBoundingBox:  {Operator: "bounding_box:", Class: XTermClassAtom},
	XTermTypeLang:         {Operator: "lang:", Class: XTermClassModifier},
	XTermTypeIs:           {Operator: "is:", Class: XTermClassModifier},
	XTermTypeHas:          {Operator: "has:", Class: XTermClassModifier},
	XTermTypeFollowers:    {Operator: "followers_count:", Class: XTermClassModifier},
	XTermTypeTweets:       {Operator: "tweets_count:", Class: XTermClassModifier},
	XTermTypeFollowing:    {Operator: "following_count:", Class: XTermClassModifier},
	XTermTypeListed:       {Operator: "listed_count:", Class: XTermClassModifier},
	XTermTypeSample:       {Operator: "sample:", Class: XTermClassModifier},
}

type XTerm struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Type      XTermType  `json:"type"`
	Class     XTermClass `json:"class"`
	Operator  string     `json:"operator"`
	Value     string     `json:"value"`
	Active    bool       `json:"active"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

// Render returns the X API syntax for this source (operator + value).
func (s *XTerm) Render() string {
	if s.Operator == "" {
		return s.Value
	}
	return s.Operator + s.Value
}
