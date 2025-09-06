package kubernetes

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
)

type PatchOperation struct {
	Op    string `json:"op"`
	Path  string `json:"path"`
	Value any    `json:"value,omitempty"`
}

var deserializer = serializer.NewCodecFactory(runtime.NewScheme()).UniversalDeserializer()

/**
 * DecodePodMutationRequest decodes a pod mutation request from the http request body.
 */
func DecodePodMutationRequest(r *http.Request) (*admissionv1.AdmissionReview, *corev1.Pod, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		slog.Error("failed to read request body", "err", err)
		return nil, nil, err
	}

	admissionReview := &admissionv1.AdmissionReview{}

	_, _, err = deserializer.Decode(body, nil, admissionReview)
	if err != nil {
		slog.Error("failed to decode admission request", "err", err)
		return nil, nil, err
	} else if admissionReview == nil || admissionReview.Request == nil {
		slog.Error("admission request is nil")
		return nil, nil, errors.New("admission request is nil")
	}
	slog.Debug("admission review", "uid", admissionReview.Request.UID)

	var pod corev1.Pod
	err = json.Unmarshal(admissionReview.Request.Object.Raw, &pod)
	if err != nil {
		slog.Error("failed to decode pod resource", "err", err)
		return nil, nil, err
	}

	return admissionReview, &pod, nil
}

func EncodeMutationPatches(review *admissionv1.AdmissionReview, patches []PatchOperation) (*admissionv1.AdmissionReview, error) {
	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return nil, err
	}

	patchType := admissionv1.PatchTypeJSONPatch
	response := &admissionv1.AdmissionReview{
		TypeMeta: review.TypeMeta,
		Response: &admissionv1.AdmissionResponse{
			UID:       review.Request.UID,
			Allowed:   true,
			Patch:     patchBytes,
			PatchType: &patchType,
		},
	}

	return response, nil
}
