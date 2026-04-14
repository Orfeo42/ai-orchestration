package gitops

//go:generate moq -out mock.go . Git:MockGit

type Git interface {
	CreateBranch(branch string) error
	Checkout(branch string) error
	Fetch() error
	CheckoutRemoteBranch(branch string) error
	Add(path string) error
	AddAll() error
	Commit(message string) error
	Push(branch string) error
	HasChanges() (bool, error)
}

type Factory func(repoPath, forgejoURL, token, owner, repo string) Git

func DefaultFactory(repoPath, forgejoURL, token, owner, repo string) Git {
	return New(repoPath, forgejoURL, token, owner, repo)
}
