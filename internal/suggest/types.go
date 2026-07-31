package suggest

type Mode string

const (
	ModeSpec    Mode = "spec"
	ModeHistory Mode = "history"
)

type Suggestion struct {
	Label       string
	Insert      string
	Description string
	Kind        string
	Score       float64
}

type specItem struct {
	Value       string `json:"value"`
	Description string `json:"description"`
}

type specFile struct {
	Root     []specItem            `json:"root"`
	Contexts map[string][]specItem `json:"contexts"`
}

type recipe struct {
	ID          string `json:"id"`
	Phrase      string `json:"phrase"`
	Description string `json:"description"`
	Workspace   bool   `json:"workspace"`
}

type recipeFile struct {
	Recipes []recipe `json:"recipes"`
}

type parseResult struct {
	Tokens       []string
	Current      string
	ReplaceStart int
	Trailing     bool
}
