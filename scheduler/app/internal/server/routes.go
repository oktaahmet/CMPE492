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
	if deps.WorkerAuth != nil {
		http.HandleFunc("/api/auth/challenge", workerAuthChallengeHandler(deps.WorkerAuth))
		http.HandleFunc("/api/auth/verify", workerAuthVerifyHandler(deps.WorkerAuth))
	}
	http.HandleFunc("/api/workers/register", registerWorkerHandler(deps.Engine, deps.WorkerAuth))
	http.HandleFunc("/api/pull", pullHandler(deps.Engine, deps.WorkerAuth))
	http.HandleFunc(
		"/api/result",
		resultHandler(deps.Engine, deps.WorkflowManager, deps.Store, deps.MaxResultPayloadBytes, deps.WorkerAuth),
	)
	http.HandleFunc("/api/workflow/node-output", workflowNodeOutputHandler(deps.Store))
	http.HandleFunc("/api/workflow/node-output/chunk", workflowNodeOutputChunkHandler(deps.Store))
	http.HandleFunc("/api/workflow/artifact", workflowArtifactHandler(deps.Store))
	http.HandleFunc("/api/payments", paymentsHandler(deps.Store, deps.WorkerAuth))
	http.HandleFunc("/api/stats", statsHandler(deps.Engine))
	http.HandleFunc("/api/runtime", runtimeHandler(deps.Engine, deps.WorkflowManager, deps.Store))
	http.HandleFunc("/healthz", healthHandler())
	http.HandleFunc("/readyz", readinessHandler(deps.Store))
	registerAdminRoutes(deps)
	http.Handle("/api/docs/", httpSwagger.WrapHandler)
	registerAdminAssets()
	http.Handle("/", http.FileServer(http.Dir("./static")))
}

func registerAdminAssets() {
	http.HandleFunc("/admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
	})
	http.Handle("/admin/", http.StripPrefix("/admin/", http.FileServer(http.Dir("./admin"))))
}

func registerAdminRoutes(deps routeDeps) {
	if deps.AdminToken == "" {
		log.Println("ADMIN_API_TOKEN not set: admin endpoints disabled")
		return
	}
	http.HandleFunc(
		"/api/admin/workflows",
		withAdminToken(deps.AdminToken, adminWorkflowListHandler(deps.WorkflowManager, deps.Store)),
	)
	http.HandleFunc(
		"/api/admin/workflows/upload",
		withAdminToken(deps.AdminToken, adminWorkflowUploadHandler(deps.WorkflowManager)),
	)
	http.HandleFunc(
		"/api/admin/workflows/activate",
		withAdminToken(deps.AdminToken, adminWorkflowActivateHandler(deps.Engine, deps.WorkflowManager, deps.Store)),
	)
	http.HandleFunc(
		"/api/admin/workflows/delete",
		withAdminToken(deps.AdminToken, adminWorkflowDeleteHandler(deps.Engine, deps.WorkflowManager, deps.Store)),
	)
	http.HandleFunc(
		"/api/admin/payments/requeue-interrupted",
		withAdminToken(deps.AdminToken, adminRequeueInterruptedPaymentsHandler(deps.Engine, deps.Store)),
	)
}
