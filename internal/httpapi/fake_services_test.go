package httpapi

import (
	"context"
	"errors"
)

// ---- Fake Auth Service ----

type fakeAuthService struct {
	users        map[string]string // email -> userID
	passwords    map[string]string // userID -> password hash (password + ":hashed")
	displayNames map[string]string // userID -> display name
	counter      int
}

func newFakeAuthService() *fakeAuthService {
	return &fakeAuthService{
		users:        make(map[string]string),
		passwords:    make(map[string]string),
		displayNames: make(map[string]string),
	}
}

func (f *fakeAuthService) Register(ctx context.Context, email, password, displayName string) (string, error) {
	if email == "" || password == "" {
		return "", errors.New("email and password required")
	}
	if _, exists := f.users[email]; exists {
		return "", errors.New("email already exists")
	}
	f.counter++
	userID := "user-" + itoa(f.counter)
	f.users[email] = userID
	f.passwords[userID] = password + ":hashed"
	f.displayNames[userID] = displayName
	return userID, nil
}

func (f *fakeAuthService) Login(ctx context.Context, email, password string) (string, string, error) {
	userID, exists := f.users[email]
	if !exists {
		return "", "", errors.New("invalid credentials")
	}
	if f.passwords[userID] != password+":hashed" {
		return "", "", errors.New("invalid credentials")
	}
	return userID, f.displayNames[userID], nil
}

// ---- Fake Projects Service ----

type fakeProjectsService struct {
	projects    map[string]Project // projectID -> project
	members     map[string]map[string]string // projectID -> userID -> role
	tokens      map[string]ProjectToken
	projCounter int
	tokCounter  int
}

func newFakeProjectsService() *fakeProjectsService {
	return &fakeProjectsService{
		projects: make(map[string]Project),
		members:  make(map[string]map[string]string),
		tokens:   make(map[string]ProjectToken),
	}
}

func (f *fakeProjectsService) CreateProject(ctx context.Context, userID, name, description string) (Project, error) {
	f.projCounter++
	id := "proj-" + itoa(f.projCounter)
	p := Project{
		ID:          id,
		Name:        name,
		Description: description,
	}
	f.projects[id] = p
	if f.members[id] == nil {
		f.members[id] = make(map[string]string)
	}
	f.members[id][userID] = "owner"
	return p, nil
}

func (f *fakeProjectsService) ListProjects(ctx context.Context, userID string) ([]Project, error) {
	var result []Project
	for _, p := range f.projects {
		if roles, ok := f.members[p.ID]; ok {
			if _, isMember := roles[userID]; isMember {
				result = append(result, p)
			}
		}
	}
	return result, nil
}

func (f *fakeProjectsService) CreateToken(ctx context.Context, projectID, userID, description string, scopes []string) (string, ProjectToken, error) {
	if _, ok := f.projects[projectID]; !ok {
		return "", ProjectToken{}, errors.New("project not found")
	}
	if roles, ok := f.members[projectID]; ok {
		if _, member := roles[userID]; !member {
			return "", ProjectToken{}, errors.New("not a project member")
		}
	}
	f.tokCounter++
	id := "tok-" + itoa(f.tokCounter)
	plainToken := "ascope_" + id + "00randomdatafortestingpurposes"
	t := ProjectToken{
		ID:          id,
		ProjectID:   projectID,
		TokenPrefix: "ascope_" + id,
		Scopes:      scopes,
		Description: description,
	}
	f.tokens[id] = t
	return plainToken, t, nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}
