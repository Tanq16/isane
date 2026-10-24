package daemon

import (
	"encoding/json/jsontext"
	"encoding/json/v2"
	"io"
	"sync"
)

type Level string

const (
	LevelInfo    Level = "info"
	LevelSuccess Level = "success"
	LevelWarn    Level = "warn"
	LevelError   Level = "error"
)

type Event struct {
	Level Level  `json:"level"`
	Msg   string `json:"msg"`
	Error string `json:"error"`
}

type reporter struct {
	mu sync.Mutex
	w  io.Writer
}

func (r *reporter) report(level Level, msg string, err error) {
	ev := Event{Level: level, Msg: msg}
	if err != nil {
		ev.Error = err.Error()
	}
	data, mErr := json.Marshal(ev, jsontext.AllowInvalidUTF8(true))
	if mErr != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.w.Write(append(data, '\n'))
}
