// Copyright 2021 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package relui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v4"
	"github.com/jackc/pgx/v4/pgxpool"
	"golang.org/x/build/internal/relui/db"
	"golang.org/x/build/internal/workflow"
	"golang.org/x/sync/errgroup"
)

type Listener interface {
	workflow.Listener

	WorkflowStarted(ctx context.Context, workflowID uuid.UUID, name string, params map[string]string) error
	WorkflowFinished(ctx context.Context, workflowID uuid.UUID, outputs map[string]interface{}, err error) error
}

// Worker runs workflows, and persists their state.
type Worker struct {
	dh *DefinitionHolder

	db *pgxpool.Pool
	l  Listener

	done    chan struct{}
	pending chan *workflow.Workflow
}

// NewWorker returns a Worker ready to accept and run workflows.
func NewWorker(dh *DefinitionHolder, db *pgxpool.Pool, l Listener) *Worker {
	return &Worker{
		dh:      dh,
		db:      db,
		l:       l,
		done:    make(chan struct{}),
		pending: make(chan *workflow.Workflow, 1),
	}
}

// Run runs started workflows, waiting for new workflows to start.
//
// On context cancellation, Run waits for all running workflows to
// finish.
func (w *Worker) Run(ctx context.Context) error {
	eg, ctx := errgroup.WithContext(ctx)
	for {
		select {
		case <-ctx.Done():
			close(w.done)
			if err := eg.Wait(); err != nil {
				return err
			}
			return ctx.Err()
		case wf := <-w.pending:
			eg.Go(func() error {
				outputs, err := wf.Run(ctx, w.l)
				if wfErr := w.l.WorkflowFinished(ctx, wf.ID, outputs, err); wfErr != nil {
					return fmt.Errorf("w.l.WorkflowFinished(_, %q, %v, %q) = %w", wf.ID, outputs, err, wfErr)
				}
				return nil
			})
		}
	}
}

func (w *Worker) run(wf *workflow.Workflow) error {
	select {
	case <-w.done:
		return errors.New("worker stopped")
	case w.pending <- wf:
		return nil
	}
}

// StartWorkflow persists and starts running a workflow.
func (w *Worker) StartWorkflow(ctx context.Context, name string, def *workflow.Definition, params map[string]string) (uuid.UUID, error) {
	wf, err := workflow.Start(def, params)
	if err != nil {
		return uuid.UUID{}, err
	}
	if err := w.l.WorkflowStarted(ctx, wf.ID, name, params); err != nil {
		return wf.ID, err
	}
	if err := w.run(wf); err != nil {
		return wf.ID, err
	}
	return wf.ID, err
}

// ResumeAll resumes all workflows with unfinished tasks.
func (w *Worker) ResumeAll(ctx context.Context) error {
	q := db.New(w.db)
	wfs, err := q.UnfinishedWorkflows(ctx)
	if err != nil {
		return fmt.Errorf("q.UnfinishedWorkflows() = _, %w", err)
	}
	for _, wf := range wfs {
		if err := w.Resume(ctx, wf.ID); err != nil {
			log.Printf("w.Resume(_, %q) = %v", wf.ID, err)
		}
	}
	return nil
}

// Resume resumes a workflow.
func (w *Worker) Resume(ctx context.Context, id uuid.UUID) error {
	var err error
	var wf db.Workflow
	var tasks []db.Task
	err = w.db.BeginFunc(ctx, func(tx pgx.Tx) error {
		q := db.New(w.db)
		wf, err = q.Workflow(ctx, id)
		if err != nil {
			return fmt.Errorf("q.Workflow(_, %v) = %w", id, err)
		}
		tasks, err = q.TasksForWorkflow(ctx, id)
		if err != nil {
			return fmt.Errorf("q.TasksForWorkflow(_, %v) = %w", id, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	d := w.dh.Definition(wf.Name.String)
	if d == nil {
		return fmt.Errorf("no workflow named %q", wf.Name.String)
	}
	state := &workflow.WorkflowState{ID: wf.ID}
	if err := json.Unmarshal([]byte(wf.Params.String), &state.Params); err != nil {
		return fmt.Errorf("unmarshalling params for %q: %w", id, err)
	}
	taskStates := make(map[string]*workflow.TaskState)
	for _, t := range tasks {
		taskStates[t.Name] = &workflow.TaskState{
			Name:             t.Name,
			Finished:         t.Finished,
			SerializedResult: []byte(t.Result.String),
			Error:            t.Error.String,
		}
	}
	res, err := workflow.Resume(d, state, taskStates)
	if err != nil {
		return err
	}
	return w.run(res)
}
