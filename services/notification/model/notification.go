package model

type Confirmation struct {
	Email string
	Token string
	Repo  string
}

type ReleaseInfo struct {
	TagName     string
	Name        string
	HTMLURL     string
	PublishedAt string
}
