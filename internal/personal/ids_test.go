package personal

import (
	"regexp"
	"testing"
	"time"

	"github.com/gookit/goutil/x/assert"
)

func TestNewSessionIDHasDatePrefix(t *testing.T) {
	day := time.Now().Format("060102")
	id := NewSessionID()
	assert.Eq(t, true, regexp.MustCompile(`^sess_`+day+`[0-9a-v]{15,}$`).MatchString(id))
	assert.NoErr(t, validateSessionID(id))
	assert.Eq(t, true, NewSessionID() != id)
}
