package domain

type CloneURL struct {
	HttpURL string `json:"http_url"`
	SshURL  string `json:"ssh_url"`
}
