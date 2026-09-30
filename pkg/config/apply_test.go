package config

import (
	"testing"

	"github.com/peterldowns/testy/assert"

	"github.com/kimura-apg/localias-v2/pkg/hostctl"
)

// fakeController is an in-memory hostctl.Controller for testing Apply
// without touching a real /etc/hosts file.
type fakeController struct {
	set []string
}

func (f *fakeController) Set(_ string, alias string) error {
	f.set = append(f.set, alias)
	return nil
}

func (f *fakeController) SetLocal(alias string) error {
	return f.Set("127.0.0.1", alias)
}

func (f *fakeController) Remove(_ string) error { return nil }
func (f *fakeController) Clear() error          { f.set = nil; return nil }
func (f *fakeController) Apply() (bool, error)  { return false, nil }
func (f *fakeController) List() (map[string][]*hostctl.Line, error) {
	return nil, nil
}

var _ hostctl.Controller = &fakeController{}

func TestApplySkipsWildcardEntries(t *testing.T) {
	t.Parallel()
	hctl := &fakeController{}
	cfg := &Config{
		Entries: []Entry{
			{Alias: "plain.localhost", Port: 9000},
			{Alias: "*.pelog.localhost", Port: 8787},
		},
	}
	err := Apply(hctl, cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"plain.localhost"}, hctl.set)
}

func TestApplyDedupesSharedHosts(t *testing.T) {
	t.Parallel()
	hctl := &fakeController{}
	cfg := &Config{
		Entries: []Entry{
			{Alias: "pelog.localhost", Port: 3437},
			{Alias: "pelog.localhost/api/*", Port: 8787},
			{Alias: "pelog.localhost/graphql", Port: 8787},
		},
	}
	err := Apply(hctl, cfg)
	assert.NoError(t, err)
	assert.Equal(t, []string{"pelog.localhost"}, hctl.set)
}
