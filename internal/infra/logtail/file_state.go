package logtail

import (
	"encoding/json"
	"fmt"
)

// filePos is one file's durable position.
type filePos struct {
	Key    string `json:"key"`
	Offset int64  `json:"offset"`
}

// fileMark is the file source's Mark: every tracked file's position after
// the lines of one poll.
type fileMark map[string]filePos

// fileState is what is persisted.
type fileState struct {
	Files map[string]filePos `json:"files"`
}

func encodeFileState(m fileMark) ([]byte, error) {
	b, err := json.Marshal(fileState{Files: m})
	if err != nil {
		return nil, fmt.Errorf("encode file state: %w", err)
	}
	return b, nil
}

func decodeFileState(b []byte) (map[string]filePos, error) {
	var st fileState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("decode file state: %w", err)
	}
	if st.Files == nil {
		st.Files = map[string]filePos{}
	}
	return st.Files, nil
}
