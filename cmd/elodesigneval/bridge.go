package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/joho/godotenv"

	"vozko/domain/copilot"
)

const (
	bridgeFirstPort    = 9391
	bridgeStartTimeout = 4 * time.Minute
	bridgeReplyBytes   = 64 << 20
)

var errBridgeBroken = errors.New("the headless editor stopped answering")

type bridgeMessage struct {
	ID       int             `json:"id"`
	Ready    bool            `json:"ready"`
	Reply    json.RawMessage `json:"reply"`
	Document json.RawMessage `json:"document"`
	Error    string          `json:"error"`
}

type editorChannel interface {
	open(ctx context.Context, project string, spec json.RawMessage) error
	command(ctx context.Context, cmd copilot.ScreenCommand) (json.RawMessage, error)
	document(ctx context.Context) (json.RawMessage, error)
	usable() bool
	close()
}

type editorBridge struct {
	cmd     *exec.Cmd
	in      io.WriteCloser
	lines   chan []byte
	mu      sync.Mutex
	next    int
	broken  bool
	stopped chan struct{}
}

func bridgePort(slot int) string {
	return strconv.Itoa(bridgeFirstPort + 2*slot)
}

func secretFreeEnv(environ []string, secrets map[string]string, port string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, entry := range environ {
		name, _, _ := strings.Cut(entry, "=")
		if _, secret := secrets[name]; secret || name == "STUDIO_HARNESS_PORT" {
			continue
		}
		out = append(out, entry)
	}
	return append(out, "STUDIO_HARNESS_PORT="+port)
}

func startBridge(frontDir, perception string, slot int) (*editorBridge, error) {
	secrets, err := godotenv.Read()
	if err != nil {
		return nil, fmt.Errorf("the backend .env could not be read to keep it out of the editor process: %w", err)
	}
	cmd := exec.Command("bun", "scripts/elo-bench/bridge.ts", "--perception="+perception)
	cmd.Dir = frontDir
	cmd.Env = secretFreeEnv(os.Environ(), secrets, bridgePort(slot))
	cmd.Stderr = os.Stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	b := &editorBridge{cmd: cmd, in: in, lines: make(chan []byte, 1), stopped: make(chan struct{})}
	go b.read(out)
	select {
	case line, ok := <-b.lines:
		var ready bridgeMessage
		if !ok || json.Unmarshal(line, &ready) != nil || !ready.Ready {
			b.close()
			return nil, errors.New("the headless editor did not start")
		}
	case <-time.After(bridgeStartTimeout):
		b.close()
		return nil, errors.New("the headless editor took too long to start")
	}
	return b, nil
}

func (b *editorBridge) read(out io.Reader) {
	defer close(b.lines)
	scanner := bufio.NewScanner(out)
	scanner.Buffer(make([]byte, 1<<20), bridgeReplyBytes)
	for scanner.Scan() {
		line := append([]byte(nil), scanner.Bytes()...)
		select {
		case b.lines <- line:
		case <-b.stopped:
			return
		}
	}
}

func (b *editorBridge) call(ctx context.Context, request map[string]interface{}) (bridgeMessage, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.broken {
		return bridgeMessage{}, errBridgeBroken
	}
	b.next++
	request["id"] = b.next
	raw, err := json.Marshal(request)
	if err != nil {
		return bridgeMessage{}, err
	}
	if _, err := b.in.Write(append(raw, '\n')); err != nil {
		b.broken = true
		return bridgeMessage{}, errBridgeBroken
	}
	select {
	case line, ok := <-b.lines:
		var message bridgeMessage
		if !ok || json.Unmarshal(line, &message) != nil || message.ID != b.next {
			b.broken = true
			return bridgeMessage{}, errBridgeBroken
		}
		if message.Error != "" {
			return message, errors.New(message.Error)
		}
		return message, nil
	case <-ctx.Done():
		b.broken = true
		return bridgeMessage{}, copilot.ErrScreenUnavailable
	}
}

func (b *editorBridge) open(ctx context.Context, project string, spec json.RawMessage) error {
	_, err := b.call(ctx, map[string]interface{}{"op": "open", "project": project, "spec": spec})
	return err
}

func (b *editorBridge) command(ctx context.Context, cmd copilot.ScreenCommand) (json.RawMessage, error) {
	message, err := b.call(ctx, map[string]interface{}{"op": "command", "command": cmd})
	return message.Reply, err
}

func (b *editorBridge) usable() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return !b.broken
}

func (b *editorBridge) document(ctx context.Context) (json.RawMessage, error) {
	message, err := b.call(ctx, map[string]interface{}{"op": "document"})
	return message.Document, err
}

func (b *editorBridge) close() {
	select {
	case <-b.stopped:
		return
	default:
		close(b.stopped)
	}
	_ = b.in.Close()
	done := make(chan struct{})
	go func() {
		_ = b.cmd.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		_ = b.cmd.Process.Kill()
		<-done
	}
}

type editorTally struct {
	looks  int
	edits  int
	failed int
}

type benchEditor struct {
	bridge  editorChannel
	project string
	tally   editorTally
}

func (e *benchEditor) Run(ctx context.Context, cmd copilot.ScreenCommand) (copilot.ScreenReply, error) {
	cmd.ID = uuid.NewString()
	cmd.ProjectID = e.project
	wait, cancel := context.WithTimeout(ctx, cmd.Name.Timeout())
	defer cancel()
	raw, err := e.bridge.command(wait, cmd)
	if err != nil {
		return copilot.ScreenReply{}, err
	}
	reply, err := copilot.DecodeScreenReply(raw)
	if err != nil {
		return copilot.ScreenReply{}, err
	}
	switch cmd.Name {
	case copilot.ScreenLook:
		e.tally.looks++
	case copilot.ScreenEdit:
		e.tally.edits++
		if !reply.OK {
			e.tally.failed++
		}
	}
	return reply, nil
}
