package confluence

import (
	"testing"

	"github.com/anacrolix/log"
)

func TestHandlerDefaultInit(t *testing.T) {
	var h Handler
	h.init()
}

func TestHandlerInitLog(t *testing.T) {
	var h Handler
	h.Logger = new(log.Logger)
	*h.Logger = log.Default.FilterLevel(log.NotSet)
	h.init()
}
