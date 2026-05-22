package server

import (
	"log"
	"net/http"

	httpSwagger "github.com/swaggo/http-swagger"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

type routeDeps struct {
	Engine                *scheduler.Engine
	WorkflowManager       *scheduler.WorkflowManager
	Store                 *postgres.Store
	WorkerAuth            *workerAuth
	MaxResultPayloadBytes int64
	AdminToken            string
}

func registerRoutes(deps routeDeps) {
	// Keep rate limiting at the route boundary so handlers stay focused on
	// validation/business logic and each endpoint group can use its own budget.
	limiter := newHTTPRateLimiter()
	if deps.WorkerAuth != nil {
		http.HandleFunc("/api/auth/challenge", withRateLimit(limiter, authRateLimitRule(), workerAuthChallengeHandler(deps.WorkerAuth)))
		http.HandleFunc("/api/auth/verify", withRateLimit(limiter, authRateLimitRule(), workerAuthVerifyHandler(deps.WorkerAuth)))
	}
	http.HandleFunc("/api/workers/register", withRateLimit(limiter, workerRateLimitRule("register"), registerWorkerHandler(deps.Engine, deps.WorkerAuth)))
	http.HandleFunc("/api/pull", withRateLimit(limiter, pullRateLimitRule(), pullHandler(deps.Engine, deps.WorkerAuth)))
	http.HandleFunc(
		"/api/result",
		withRateLimit(
			limiter,
			workerRateLimitRule("result"),
			resultHandler(deps.Engine, deps.WorkflowManager, deps.Store, deps.MaxResultPayloadBytes, deps.WorkerAuth),
		),
	)
	http.HandleFunc("/api/workflow/node-output", withRateLimit(limiter, artifactRateLimitRule("node-output"), workflowNodeOutputHandler(deps.Store)))
	http.HandleFunc("/api/workflow/node-output/chunk", withRateLimit(limiter, artifactRateLimitRule("node-output-chunk"), workflowNodeOutputChunkHandler(deps.Store)))
	http.HandleFunc("/api/workflow/artifact", withRateLimit(limiter, artifactRateLimitRule("artifact"), workflowArtifactHandler(deps.Store)))
	http.HandleFunc("/api/payments", withRateLimit(limiter, readRateLimitRule("payments"), paymentsHandler(deps.Store, deps.WorkerAuth)))
	http.HandleFunc("/api/stats", withRateLimit(limiter, readRateLimitRule("stats"), statsHandler(deps.Engine)))
	http.HandleFunc("/api/runtime", withRateLimit(limiter, readRateLimitRule("runtime"), runtimeHandler(deps.Engine, deps.WorkflowManager, deps.Store)))
	http.HandleFunc("/healthz", healthHandler())
	http.HandleFunc("/readyz", readinessHandler(deps.Store))
	registerAdminRoutes(deps, limiter)
	http.Handle("/api/docs/", httpSwagger.WrapHandler)
	http.Handle("/", http.FileServer(http.Dir("./static")))
}

func registerAdminRoutes(deps routeDeps, limiter *httpRateLimiter) {
	if deps.AdminToken == "" {
		log.Println("ADMIN_API_TOKEN not set: admin endpoints disabled")
		return
	}
	http.HandleFunc(
		"/api/admin/workflows",
		withRateLimit(limiter, adminRateLimitRule(), withAdminToken(deps.AdminToken, adminWorkflowListHandler(deps.WorkflowManager, deps.Store))),
	)
	http.HandleFunc(
		"/api/admin/workflows/upload",
		withRateLimit(limiter, adminRateLimitRule(), withAdminToken(deps.AdminToken, adminWorkflowUploadHandler(deps.WorkflowManager))),
	)
	http.HandleFunc(
		"/api/admin/workflows/activate",
		withRateLimit(limiter, adminRateLimitRule(), withAdminToken(deps.AdminToken, adminWorkflowActivateHandler(deps.Engine, deps.WorkflowManager, deps.Store))),
	)
	http.HandleFunc(
		"/api/admin/workflows/delete",
		withRateLimit(limiter, adminRateLimitRule(), withAdminToken(deps.AdminToken, adminWorkflowDeleteHandler(deps.Engine, deps.WorkflowManager, deps.Store))),
	)
	http.HandleFunc(
		"/api/admin/payments/requeue-interrupted",
		withRateLimit(limiter, adminRateLimitRule(), withAdminToken(deps.AdminToken, adminRequeueInterruptedPaymentsHandler(deps.Engine, deps.Store))),
	)
}
