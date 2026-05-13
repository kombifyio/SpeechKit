package main

import "net/http"

func registerControlPlaneAPIRoutes(mux *http.ServeMux, deps controlPlaneDeps) {
	registerAPIV1Routes(mux, deps.ConfigPath, deps.Config, deps.State, deps.STTRouter, deps.FeedbackStore)
}
