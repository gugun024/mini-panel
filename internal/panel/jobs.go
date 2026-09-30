package panel

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

type jobLogger func(format string, args ...any)

func (a *App) handleJobs(w http.ResponseWriter, r *http.Request) {
	jobs, err := a.store.ListJobs(r.Context())
	if err != nil {
		http.Error(w, "could not list jobs", http.StatusInternalServerError)
		return
	}
	a.render(w, http.StatusOK, "jobs", pageData{
		Session: sessionFromContext(r.Context()),
		Jobs:    jobs,
	})
}

func (a *App) handleJobDetail(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	job, err := a.store.GetJob(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	a.render(w, http.StatusOK, "job_detail", pageData{
		Session: sessionFromContext(r.Context()),
		Job:     job,
	})
}

func (a *App) createDeployJob(w http.ResponseWriter, r *http.Request, jobType, domain string, work func(context.Context, jobLogger) error) {
	job, err := a.store.CreateJob(r.Context(), jobType, domain)
	if err != nil {
		a.renderDomainsWithMessage(w, r, http.StatusInternalServerError, "Gagal membuat job deploy.", "")
		return
	}
	go a.runJob(job.ID, work)
	http.Redirect(w, r, "/jobs/"+strconv.FormatInt(job.ID, 10), http.StatusSeeOther)
}

func (a *App) runJob(jobID int64, work func(context.Context, jobLogger) error) {
	ctx := context.Background()
	_ = a.store.StartJob(ctx, jobID)
	logf := func(format string, args ...any) {
		line := fmt.Sprintf(format, args...)
		if line == "" {
			return
		}
		_ = a.store.AppendJobLog(ctx, jobID, time.Now().UTC().Format("15:04:05")+" "+line+"\n")
	}
	logf("job started")
	if err := work(ctx, logf); err != nil {
		logf("failed: %v", err)
		_ = a.store.FailJob(ctx, jobID, err.Error())
		return
	}
	logf("job finished")
	_ = a.store.FinishJob(ctx, jobID)
}
