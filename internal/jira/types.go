package jira

import "time"

type User struct {
	AccountID   string `json:"accountId"`
	DisplayName string `json:"displayName"`
	// Set by the user-search endpoints only (zero values elsewhere).
	AccountType string `json:"accountType,omitempty"` // atlassian | app | customer
	Active      *bool  `json:"active,omitempty"`
}

type Named struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type StatusCategory struct {
	Key string `json:"key"` // new | indeterminate | done
}

type Status struct {
	ID       string         `json:"id"`
	Name     string         `json:"name"`
	Category StatusCategory `json:"statusCategory"`
}

type Comment struct {
	ID      string    `json:"id"`
	Author  User      `json:"author"`
	Body    any       `json:"body"` // ADF document
	Created time.Time `json:"-"`
	RawDate string    `json:"created"`
	// Public is Jira Service Management's jsdPublic: true = visible to the customer,
	// false = internal. nil when Jira does not say (non JSM project).
	Public *bool `json:"jsdPublic"`
}

type Fields struct {
	Summary     string   `json:"summary"`
	Description any      `json:"description"` // ADF document
	Status      Status   `json:"status"`
	IssueType   Named    `json:"issuetype"`
	Priority    *Named   `json:"priority"`
	Assignee    *User    `json:"assignee"`
	Reporter    *User    `json:"reporter"`
	Updated     string   `json:"updated"`
	Labels      []string `json:"labels"`
	Created     string   `json:"created"`
	Components  []Named  `json:"components"`
	// OriginalEstimate is the original time estimate in seconds (nil if unset).
	OriginalEstimate *int `json:"timeoriginalestimate"`
	Comment          struct {
		Comments []Comment `json:"comments"`
	} `json:"comment"`
}

type Issue struct {
	// CommentsErr is set when the comments could not be loaded (the issue itself still is).
	CommentsErr string `json:"-"`
	ID          string `json:"id"`
	Key         string `json:"key"`
	Fields      Fields `json:"fields"`
}

type Transition struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	To   Status `json:"to"`
}

type Board struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Type string `json:"type"`
}

// Column is a board column and the status IDs mapped to it.
type Column struct {
	Name     string
	StatusID []string
}

// Doc is an Atlassian Document Format document (what Jira stores for descriptions and comments).
type Doc = map[string]any

// IssueRef is an issue found by the ticket picker.
type IssueRef struct {
	Key     string
	Summary string
}

// EditMeta says what can be edited on an issue (GET /issue/{key}/editmeta).
type EditMeta struct {
	Priorities []Named         // allowed priorities (the project's scheme)
	Editable   map[string]bool // field keys Jira lets the user edit: summary, description, priority, labels…
}

// FieldUpdate lists the fields to change; nil fields are left untouched.
type FieldUpdate struct {
	Summary     *string
	Description *Doc // a document with no content clears the description
	PriorityID  *string
	Labels      *[]string
}

// Service is the surface the UI depends on; implemented by Client and mock.
type Service interface {
	Search(jql string) ([]Issue, error)
	GetIssue(key string) (Issue, error)
	Transitions(key string) ([]Transition, error)
	DoTransition(key, transitionID string) error
	// AddComment posts a comment; internal=true hides it from the customer (Jira Service Management).
	AddComment(key string, body Doc, internal bool) error
	AssignToMe(key string) error
	Unassign(key string) error
	// AssignTo gives the issue to accountID.
	AssignTo(key, accountID string) error
	// SearchAssignable returns the users that can be assigned to the issue and match query.
	SearchAssignable(key, query string) ([]User, error)
	// Myself returns the authenticated user.
	Myself() (User, error)
	EditMeta(key string) (EditMeta, error)
	UpdateIssue(key string, u FieldUpdate) error
	// EditComment replaces the body of a comment; the customer visibility (Jira Service
	// Management) of c is sent again so that an edit never changes it.
	EditComment(key string, c Comment, body Doc) error
	// SearchMentionable returns the people who can see the issue and match query (for "@").
	SearchMentionable(key, query string) ([]User, error)
	// SearchIssues returns issues matching query, the recent ones when it is empty (for "#").
	SearchIssues(query string) ([]IssueRef, error)
	DeleteComment(key, commentID string) error
	Boards() ([]Board, error)
	BoardColumns(boardID int) ([]Column, error)
	BoardIssues(boardID int) ([]Issue, error)
	BrowseURL(key string) string
}
