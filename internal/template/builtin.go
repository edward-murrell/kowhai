// Package template defines the built-in board templates. The service
// layer seeds them into a workspace on request and copies them like any
// other template board.
package template

// Board is a built-in board template.
type Board struct {
	Key         string
	Name        string
	Description string
	Lists       []List
	Labels      []Label
	// StatusLists maps a Claude Code status to a list name. When set the
	// status mapping is turned on for boards made from the template.
	StatusLists map[string]string
}

type List struct {
	Name  string
	Cards []Card
}

type Label struct{ Name, Colour string }

type Card struct {
	Title       string
	Description string
	IsTemplate  bool
	Labels      []string
	Checklists  []Checklist
}

type Checklist struct {
	Title string
	Items []string
}

// Builtins lists the templates Kōwhai ships with.
var Builtins = []Board{
	{
		Key:         "kanban",
		Name:        "Kanban",
		Description: "A simple flow from to do to done.",
		Lists:       []List{{Name: "To do"}, {Name: "Doing"}, {Name: "Done"}},
		Labels:      defaultLabels(),
	},
	{
		Key:         "sprint",
		Name:        "Sprint",
		Description: "Plan and track a fixed-length sprint.",
		Lists: []List{
			{Name: "Backlog", Cards: []Card{{
				Title: "Story", IsTemplate: true,
				Description: "**As a** …\n**I want** …\n**So that** …",
				Checklists:  []Checklist{{Title: "Acceptance criteria", Items: []string{"Criterion one"}}},
			}}},
			{Name: "Sprint"}, {Name: "In progress"}, {Name: "Review"}, {Name: "Done"},
		},
		Labels: []Label{{"Bug", "red"}, {"Feature", "green"}, {"Chore", "blue"}, {"Blocked", "orange"}},
	},
	{
		Key:         "personal",
		Name:        "Personal to-do",
		Description: "Today, this week and later.",
		Lists:       []List{{Name: "Today"}, {Name: "This week"}, {Name: "Later"}, {Name: "Done"}},
		Labels:      []Label{{"Home", "green"}, {"Work", "blue"}, {"Errand", "yellow"}},
	},
	{
		Key:         "agent",
		Name:        "Agent projects",
		Description: "Track Claude Code sessions. Cards move as sessions start work, wait for you, or finish a turn.",
		Lists: []List{
			{Name: "Backlog", Cards: []Card{{
				Title: "Agent task", IsTemplate: true,
				Description: "## Goal\n\n…\n\n## Context\n\n…\n\n## Done when\n\n…",
				Labels:      []string{"Claude"},
				Checklists:  []Checklist{{Title: "Steps", Items: []string{"Plan", "Implement", "Test", "Review"}}},
			}}},
			{Name: "Working"}, {Name: "Waiting on me"}, {Name: "Review"}, {Name: "Done"},
		},
		Labels:      []Label{{"Claude", "purple"}, {"Bug", "red"}, {"Feature", "green"}},
		StatusLists: map[string]string{"working": "Working", "waiting": "Waiting on me", "idle": "Review"},
	},
}

func defaultLabels() []Label {
	return []Label{{"", "green"}, {"", "yellow"}, {"", "orange"}, {"", "red"}, {"", "purple"}, {"", "blue"}}
}

// Find returns the built-in template with key.
func Find(key string) (Board, bool) {
	for _, b := range Builtins {
		if b.Key == key {
			return b, true
		}
	}
	return Board{}, false
}
