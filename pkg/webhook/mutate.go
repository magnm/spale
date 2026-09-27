package webhook

import (
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/render"
	"github.com/magnm/spale/pkg/kubernetes"
)

func HandleRequest(w http.ResponseWriter, r *http.Request) {
	slog.Debug("admission request", "length", r.ContentLength)
	review, pod, err := kubernetes.DecodePodMutationRequest(r)
	if err != nil {
		slog.Error("failed to decode pod mutation request", "err", err)
		http.Error(w, "failed to decode pod mutation request", http.StatusBadRequest)
		return
	}
	slog.Debug("admission review", "version", review.APIVersion, "name", pod.Name, "namespace", pod.Namespace, "dryRun", review.Request.DryRun)

	patches, err := patchesForPod(pod, review.Request.Operation, *review.Request.DryRun)
	var noNodes *kubernetes.ErrNoNodesAvailable
	if errors.As(err, &noNodes) {
		slog.Info("rejecting pod, no nodes available", "name", pod.Name, "generateName", pod.GenerateName, "namespace", pod.Namespace, "reason", err)
		// Must be an explicit denial, an HTTP error would be admitted due to failurePolicy: Ignore
		render.JSON(w, r, kubernetes.EncodeRejection(review, "spale: "+err.Error()))
		return
	}
	if err != nil {
		slog.Error("failed to generate patches for pod", "err", err)
		http.Error(w, "failed to generate patches for pod", http.StatusInternalServerError)
		return
	}

	response, err := kubernetes.EncodeMutationPatches(review, patches)
	if err != nil {
		slog.Error("failed to encode mutation patches", "err", err)
		http.Error(w, "failed to encode mutation patches", http.StatusInternalServerError)
		return
	}
	slog.Debug("admission response", "patches", patches)

	render.JSON(w, r, response)
}
