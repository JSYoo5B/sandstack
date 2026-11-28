package compute

import (
	"bytes"
	"strings"
	"unicode/utf8"
)

// FixtureConsoleOutput is the console log of the seeded server.
const FixtureConsoleOutput = "[    0.000000] Linux version 6.8.0-sandstack (fixture@sandstack) #1 SMP\n" +
	"[    1.000000] Run /sbin/init as init process\n" +
	"cloud-init: modules:final finished\n" +
	"fixture-server login: "

// ConsoleOutput returns the console log, or its last lines when length is set.
// A negative length skips that many leading lines, as Nova's tail slice does.
func (s *Service) ConsoleOutput(projectID, serverID string, length *int64) (string, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	record, ok := s.find(projectID, serverID)
	if !ok {
		return "", ErrServerNotFound
	}
	output := record.console
	if length != nil {
		output = tailLines(output, *length)
	}
	return decodeASCII(output), nil
}

// SetConsoleOutput replaces the console log, or restores the initial log when output is nil.
func (s *Service) SetConsoleOutput(serverID string, output []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	record, ok := s.servers[serverID]
	if !ok {
		return ErrServerNotFound
	}
	if output == nil {
		record.console = s.initialConsole(record.server.ID)
		return nil
	}
	record.console = bytes.Clone(output)
	return nil
}

func tailLines(output []byte, length int64) []byte {
	if length == 0 {
		return nil
	}
	lines := bytes.Split(output, []byte("\n"))
	count := int64(len(lines))
	start := count - length
	if length < 0 {
		start = -length
	}
	start = max(0, min(start, count))
	return bytes.Join(lines[start:], []byte("\n"))
}

func decodeASCII(output []byte) string {
	var decoded strings.Builder
	for _, b := range output {
		if b < utf8.RuneSelf {
			decoded.WriteByte(b)
		} else {
			decoded.WriteRune(utf8.RuneError)
		}
	}
	return decoded.String()
}

// initialConsole returns the startup log of the seeded server and an empty log otherwise.
func (s *Service) initialConsole(serverID string) []byte {
	if serverID == s.seed.ID {
		return []byte(FixtureConsoleOutput)
	}
	return []byte{}
}
