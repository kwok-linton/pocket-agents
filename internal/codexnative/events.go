package codexnative

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"pocketagents/internal/core"
)

func rawString(m map[string]json.RawMessage, key string) string {
	var s string
	_ = json.Unmarshal(m[key], &s)
	return s
}
func requestKey(id json.RawMessage) string {
	var s string
	if json.Unmarshal(id, &s) == nil {
		return "s:" + s
	}
	return "n:" + string(id)
}
func (p *Provider) event(m rpcMessage) {
	var fields map[string]json.RawMessage
	if json.Unmarshal(m.Params, &fields) != nil {
		return
	}
	id := rawString(fields, "threadId")
	p.mu.Lock()
	defer p.mu.Unlock()
	if m.Method == "serverRequest/resolved" {
		key := p.epoch + ":" + requestKey(fields["requestId"])
		if req := p.pending[key]; req != nil {
			close(req.Resolved)
			delete(p.pending, key)
		}
		return
	}
	if len(m.ID) > 0 && id != "" {
		key := p.epoch + ":" + requestKey(m.ID)
		if _, exists := p.pending[key]; exists {
			return
		}
		a := core.Approval{ID: key, NativeID: id, ExecutionID: p.execution(id), Kind: m.Method, Title: "Native Codex request", Detail: string(m.Params), Data: m.Params}
		switch m.Method {
		case "item/commandExecution/requestApproval":
			a.Title = "Approve command"
			a.Detail = rawString(fields, "command")
			if why := rawString(fields, "reason"); why != "" {
				a.Detail += "\n" + why
			}
			var decisions []json.RawMessage
			_ = json.Unmarshal(fields["availableDecisions"], &decisions)
			for _, d := range decisions {
				var v string
				if json.Unmarshal(d, &v) == nil && (v == "accept" || v == "decline" || v == "cancel") {
					a.Options = append(a.Options, v)
				}
			}
			if len(decisions) == 0 {
				a.Options = []string{"accept", "decline", "cancel"}
			}
		case "item/fileChange/requestApproval":
			a.Title = "Approve file changes"
			a.Options = []string{"accept", "decline", "cancel"}
		case "item/tool/requestUserInput":
			a.Title = "Answer native question"
			a.Options = []string{"answer"}
		default:
			a.Title = "Respond in native terminal"
			a.Options = []string{"open-terminal"}
		}
		p.pending[key] = &pendingRequest{Approval: a, RPCID: m.ID, Params: fields, TurnID: rawString(fields, "turnId"), Resolved: make(chan struct{})}
		s := p.sessions[id]
		s.TurnState = "waiting_approval"
		if m.Method == "item/tool/requestUserInput" {
			var blocking bool
			_ = json.Unmarshal(fields["isBlocking"], &blocking)
			if blocking {
				s.TurnState = "waiting_input"
			} else {
				s.TurnState = p.sessions[id].TurnState
			}
		}
		s.UpdatedAt = time.Now()
		p.sessions[id] = s
		return
	}
	s, ok := p.sessions[id]
	if !ok {
		return
	}
	switch m.Method {
	case "turn/started", "turn/completed":
		var t nativeTurn
		if json.Unmarshal(fields["turn"], &t) == nil {
			s.TurnID = t.ID
			s.TurnState = turnState(t.Status)
			s.Error = ""
			if t.Error != nil {
				s.Error = t.Error.Message
			}
			if m.Method == "turn/completed" {
				for key, req := range p.pending {
					if req.Approval.NativeID == id && req.TurnID == t.ID {
						close(req.Resolved)
						delete(p.pending, key)
					}
				}
			}
		}
	case "thread/status/changed":
		var status struct {
			Type  string   `json:"type"`
			Flags []string `json:"activeFlags"`
		}
		if json.Unmarshal(fields["status"], &status) == nil {
			switch status.Type {
			case "active":
				s.TurnState = "working"
				for _, flag := range status.Flags {
					if flag == "waitingOnApproval" {
						s.TurnState = "waiting_approval"
					}
					if flag == "waitingOnUserInput" {
						s.TurnState = "waiting_input"
					}
				}
			case "notLoaded":
				s.ExecutionState = "exited"
				s.ExecutionID = ""
				delete(p.subscribed, id)
			case "systemError":
				s.TurnState = "error"
			}
		}
	case "error":
		var retrying bool
		_ = json.Unmarshal(fields["willRetry"], &retrying)
		if !retrying {
			s.TurnState = "error"
		}
		var er struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(fields["error"], &er) == nil {
			s.Error = er.Message
		}
	}
	s.UpdatedAt = time.Now()
	p.sessions[id] = s
}
func (p *Provider) approve(ctx context.Context, r *rpcClient, a core.Action, res core.Result) (core.Result, error) {
	p.mu.Lock()
	req := p.pending[a.RequestID]
	if req == nil || req.Responded || req.Approval.NativeID != a.NativeID || req.Approval.ExecutionID != a.ExecutionID {
		p.mu.Unlock()
		return res, errors.New("approval is stale, already answered, or belongs to another execution")
	}
	p.mu.Unlock()
	var answer any
	switch req.Approval.Kind {
	case "item/commandExecution/requestApproval", "item/fileChange/requestApproval":
		allowed := false
		for _, v := range req.Approval.Options {
			if v == a.Decision {
				allowed = true
			}
		}
		if !allowed {
			return res, errors.New("decision is not offered by this native approval")
		}
		answer = map[string]string{"decision": a.Decision}
	case "item/tool/requestUserInput":
		var questions []struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(req.Params["questions"], &questions) != nil {
			return res, errors.New("invalid native question schema")
		}
		answers := map[string]map[string][]string{}
		if len(a.Data) > 0 {
			var payload struct {
				Answers map[string]map[string][]string `json:"answers"`
			}
			if err := json.Unmarshal(a.Data, &payload); err != nil {
				return res, err
			}
			answers = payload.Answers
		} else if len(questions) == 1 && strings.TrimSpace(a.Text) != "" {
			answers[questions[0].ID] = map[string][]string{"answers": {a.Text}}
		} else {
			return res, errors.New("provide an answer for each native question in action.data.answers, or use the original terminal")
		}
		for _, q := range questions {
			if len(answers[q.ID]["answers"]) == 0 {
				return res, fmt.Errorf("missing answer for native question %s", q.ID)
			}
		}
		if len(answers) != len(questions) {
			return res, errors.New("answer contains unknown question IDs")
		}
		answer = map[string]any{"answers": answers}
	default:
		return res, errors.New("this native request requires the original terminal; use the terminal command")
	}
	p.mu.Lock()
	current := p.pending[a.RequestID]
	if current != req || req.Responded {
		p.mu.Unlock()
		return res, errors.New("approval was resolved by another client")
	}
	req.Responded = true
	p.mu.Unlock()
	if err := r.send(map[string]any{"id": json.RawMessage(req.RPCID), "result": answer}); err != nil {
		res.Status = "unknown"
		res.Message = "approval delivery unknown: " + err.Error()
		return res, nil
	}
	res.ExecutionID = a.ExecutionID
	timer := time.NewTimer(4 * time.Second)
	defer timer.Stop()
	select {
	case <-req.Resolved:
		res.Status = "accepted"
		res.Message = "Native request resolved"
	case <-r.done:
		res.Status = "unknown"
		res.Message = "Control connection closed before native acknowledgement"
	case <-ctx.Done():
		res.Status = "unknown"
		res.Message = "Waiting for native request resolution"
	case <-timer.C:
		res.Status = "unknown"
		res.Message = "Reply written; native resolution not yet observed. Do not resend blindly."
	}
	return res, nil
}
