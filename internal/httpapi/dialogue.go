package httpapi

import (
	"context"
	"log"
	"maps"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/rf-us/zhihai-radar/internal/dialogue"
	"github.com/rf-us/zhihai-radar/internal/service"
	"github.com/rf-us/zhihai-radar/internal/store"
)

type dialogueSession struct {
	mu     sync.Mutex
	inputs dialogue.Session
	view   dialogueView
	cancel context.CancelFunc
}

type dialogueView struct {
	ID            string              `json:"session_id"`
	Revision      uint64              `json:"revision"`
	Status        string              `json:"status"`
	Slots         dialogue.Answers    `json:"slots"`
	Questions     []dialogue.Question `json:"questions"`
	Messages      []dialogueMessage   `json:"messages"`
	Analysis      *dialogueAnalysis   `json:"analysis,omitempty"`
	OpportunityID int64               `json:"opportunity_id,omitempty"`
}

type dialogueMessage struct {
	Kind    string   `json:"kind"`
	Text    string   `json:"text"`
	Changed []string `json:"changed,omitempty"`
}

type dialogueAnalysis struct {
	Status  string                          `json:"status"`
	Mode    string                          `json:"mode"`
	Sources map[string]store.SourceProgress `json:"sources"`
	Error   string                          `json:"error,omitempty"`
}

func newDialogue() (*dialogueSession, error) {
	id, err := newSessionID()
	if err != nil {
		return nil, err
	}
	conversation := &dialogueSession{
		view: dialogueView{
			ID: id, Revision: 1, Status: "collecting", Questions: dialogue.Questions(),
			Messages: []dialogueMessage{},
		},
	}
	conversation.view.Slots = conversation.inputs.Answers()
	return conversation, nil
}

func (conversation *dialogueSession) snapshot() dialogueView {
	view := conversation.view
	view.Messages = slices.Clone(view.Messages)
	if view.Analysis != nil {
		analysis := *view.Analysis
		analysis.Sources = maps.Clone(analysis.Sources)
		view.Analysis = &analysis
	}
	return view
}

func (s *Server) openDialogue(w http.ResponseWriter, _ *http.Request, sess *session) {
	w.Header().Set("Cache-Control", "no-store")
	sess.dialogueMu.Lock()
	defer sess.dialogueMu.Unlock()
	if sess.closed {
		writeErr(w, http.StatusUnauthorized, "unauthorized")
		return
	}
	if sess.dialogue == nil {
		created, err := newDialogue()
		if err != nil {
			writeServerErr(w, err)
			return
		}
		sess.dialogue = created
	}
	s.readDialogue(w, nil, sess.dialogue)
}

func (s *Server) withDialogue(next func(http.ResponseWriter, *http.Request, *dialogueSession)) func(http.ResponseWriter, *http.Request, *session) {
	return func(w http.ResponseWriter, r *http.Request, sess *session) {
		w.Header().Set("Cache-Control", "no-store")
		sess.dialogueMu.Lock()
		conversation := sess.dialogue
		sess.dialogueMu.Unlock()
		if conversation == nil || conversation.view.ID != r.PathValue("id") {
			writeErr(w, http.StatusNotFound, "对话已失效，请重新开始")
			return
		}
		next(w, r, conversation)
	}
}

func (s *Server) readDialogue(w http.ResponseWriter, _ *http.Request, conversation *dialogueSession) {
	conversation.mu.Lock()
	view := conversation.snapshot()
	conversation.mu.Unlock()
	writeJSON(w, http.StatusOK, view)
}

type dialogueRevision struct {
	Revision uint64 `json:"revision"`
}

func (conversation *dialogueSession) checkRevision(w http.ResponseWriter, revision uint64) bool {
	if revision != conversation.view.Revision {
		writeErr(w, http.StatusConflict, "对话已有更新，请确认最新内容后再操作")
		return false
	}
	return true
}

func (conversation *dialogueSession) editable(w http.ResponseWriter, revision uint64) bool {
	if !conversation.checkRevision(w, revision) {
		return false
	}
	if conversation.view.Status != "collecting" && conversation.view.Status != "ready" {
		writeErr(w, http.StatusConflict, "当前无法修改对话")
		return false
	}
	return true
}

func decodeDialogue(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 12<<10)
	if err := decodeJSON(r, dst); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return false
	}
	return true
}

func (s *Server) turnDialogue(w http.ResponseWriter, r *http.Request, conversation *dialogueSession) {
	var body struct {
		dialogueRevision
		Text string `json:"text"`
	}
	if !decodeDialogue(w, r, &body) {
		return
	}
	body.Text = strings.TrimSpace(body.Text)
	if body.Text == "" || utf8.RuneCountInString(body.Text) > 1000 {
		writeErr(w, http.StatusBadRequest, "消息需在 1–1000 个字符之间")
		return
	}
	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	if !conversation.editable(w, body.Revision) {
		return
	}
	reply := conversation.inputs.Turn(body.Text)
	conversation.view.Messages = append(conversation.view.Messages, dialogueMessage{Kind: "user", Text: body.Text})
	conversation.applyReply(reply)
	writeJSON(w, http.StatusOK, conversation.view)
}

func (s *Server) editDialogue(w http.ResponseWriter, r *http.Request, conversation *dialogueSession) {
	var body struct {
		dialogueRevision
		Value *string `json:"value"`
	}
	if !decodeDialogue(w, r, &body) {
		return
	}
	if body.Value == nil {
		writeErr(w, http.StatusBadRequest, "缺少槽位值")
		return
	}
	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	if !conversation.editable(w, body.Revision) {
		return
	}
	if err := conversation.inputs.Update(r.PathValue("slot"), *body.Value); err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	conversation.applyReply(conversation.inputs.Reply())
	writeJSON(w, http.StatusOK, conversation.view)
}

func (conversation *dialogueSession) applyReply(reply dialogue.Reply) {
	view := &conversation.view
	newSlots := conversation.inputs.Answers()
	message := dialogueMessage{Kind: reply.Kind, Text: reply.Text}
	for _, question := range view.Questions {
		if newSlots[question.Key] != "" && newSlots[question.Key] != view.Slots[question.Key] {
			labels := make([]string, 0, 1)
			for _, value := range strings.Split(newSlots[question.Key], ",") {
				label := value
				for _, option := range question.Options {
					if value == option.Value {
						label = option.Label
						break
					}
				}
				labels = append(labels, label)
			}
			message.Changed = append(message.Changed, question.Title+"="+strings.Join(labels, "、"))
		}
	}
	view.Slots = newSlots
	view.Messages = append(view.Messages, message)
	if len(view.Messages) > 60 {
		view.Messages = slices.Clone(view.Messages[len(view.Messages)-60:])
	}
	view.Revision++
	if reply.Kind == dialogue.KindRefused {
		return
	}
	view.Analysis = nil
	view.Status = "collecting"
	if reply.Kind == dialogue.KindReady {
		view.Status = "ready"
	}
}

func (s *Server) startDialogueAnalysis(w http.ResponseWriter, r *http.Request, conversation *dialogueSession) {
	var body dialogueRevision
	if !decodeDialogue(w, r, &body) {
		return
	}
	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	if !conversation.checkRevision(w, body.Revision) {
		return
	}
	reply := conversation.inputs.Reply()
	if conversation.view.Status != "ready" || reply.Plan == nil {
		writeErr(w, http.StatusConflict, "对话尚未就绪或已有分析任务")
		return
	}
	mode := s.analyzer.SourceMode()
	if mode == "zhihu" && !s.allowAnalysis(w, r) {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	conversation.cancel = cancel
	view := &conversation.view
	view.Revision++
	view.Status = "analyzing"
	view.Analysis = &dialogueAnalysis{
		Status: "running", Mode: mode,
		Sources: map[string]store.SourceProgress{
			service.ZhihuSearch: {Status: "pending"}, service.GlobalSearch: {Status: "pending"},
			service.Hot: {Status: "pending"}, service.Answer: {Status: "pending"},
		},
	}
	plan := reply.Plan
	go s.runDialogueAnalysis(ctx, cancel, conversation, view.Revision, service.AnalyzeInput{
		Keyword: plan.Keyword, Category: plan.Category, TargetPrice: plan.TargetCNY, Plan: plan,
	})
	writeJSON(w, http.StatusAccepted, view)
}

func (s *Server) runDialogueAnalysis(ctx context.Context, cancel context.CancelFunc, conversation *dialogueSession, revision uint64, input service.AnalyzeInput) {
	defer cancel()
	detail, err := s.analyzer.Prepare(ctx, input, func(name string, update store.SourceProgress) {
		conversation.mu.Lock()
		defer conversation.mu.Unlock()
		if conversation.view.Revision == revision {
			conversation.view.Analysis.Sources[name] = update
		}
	})

	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	view := &conversation.view
	if view.Revision != revision {
		return
	}
	view.Revision++
	conversation.cancel = nil

	message := ""
	if ctx.Err() != nil {
		message = "分析超时，请重试"
		for name, update := range view.Analysis.Sources {
			if update.Status == "pending" || update.Status == "running" {
				view.Analysis.Sources[name] = store.SourceProgress{Status: "failed", Reason: "分析超时"}
			}
		}
	} else if err != nil {
		log.Printf("dialogue: prepare analysis: %v", err)
		message = "分析未完成，请重试"
	} else if view.Analysis.Sources[service.ZhihuSearch].Status == "failed" &&
		view.Analysis.Sources[service.GlobalSearch].Status == "failed" {
		message = "两路搜索均未取得数据，请检查下方原因后重试"
	} else if err := s.analyzer.Save(ctx, detail); err != nil {
		log.Printf("dialogue: save analysis: %v", err)
		message = "保存分析结果失败，请重试"
	}
	if message != "" {
		view.Status = "ready"
		view.Analysis.Status = "failed"
		view.Analysis.Error = message
		return
	}
	view.Status = "done"
	view.Analysis.Status = "succeeded"
	view.OpportunityID = detail.Opportunity.ID
}

func (s *Server) cancelDialogueAnalysis(w http.ResponseWriter, r *http.Request, conversation *dialogueSession) {
	var body dialogueRevision
	if !decodeDialogue(w, r, &body) {
		return
	}
	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	if conversation.view.Status == "done" {
		writeJSON(w, http.StatusOK, conversation.view)
		return
	}
	if !conversation.checkRevision(w, body.Revision) {
		return
	}
	conversation.stop()
	writeJSON(w, http.StatusOK, conversation.view)
}

func (conversation *dialogueSession) stop() {
	if conversation.cancel == nil {
		return
	}
	conversation.cancel()
	conversation.cancel = nil
	view := &conversation.view
	view.Status = "ready"
	view.Revision++
	view.Analysis.Status = "canceled"
	for name, update := range view.Analysis.Sources {
		if update.Status == "pending" || update.Status == "running" {
			view.Analysis.Sources[name] = store.SourceProgress{Status: "canceled"}
		}
	}
}

func (sess *session) closeDialogue() {
	sess.dialogueMu.Lock()
	defer sess.dialogueMu.Unlock()
	sess.closed = true
	if conversation := sess.dialogue; conversation != nil {
		conversation.mu.Lock()
		conversation.stop()
		conversation.view.Revision++
		conversation.view.Status = "closed"
		conversation.mu.Unlock()
		sess.dialogue = nil
	}
}

func (s *Server) nextDialogue(w http.ResponseWriter, r *http.Request, sess *session) {
	w.Header().Set("Cache-Control", "no-store")
	var body dialogueRevision
	if !decodeDialogue(w, r, &body) {
		return
	}
	sess.dialogueMu.Lock()
	defer sess.dialogueMu.Unlock()
	conversation := sess.dialogue
	if conversation == nil || conversation.view.ID != r.PathValue("id") {
		writeErr(w, http.StatusNotFound, "对话已失效，请重新开始")
		return
	}
	conversation.mu.Lock()
	defer conversation.mu.Unlock()
	if !conversation.checkRevision(w, body.Revision) {
		return
	}
	if conversation.view.Status != "done" {
		writeErr(w, http.StatusConflict, "当前对话尚未完成")
		return
	}
	created, err := newDialogue()
	if err != nil {
		writeServerErr(w, err)
		return
	}
	sess.dialogue = created
	writeJSON(w, http.StatusCreated, created.view)
}
