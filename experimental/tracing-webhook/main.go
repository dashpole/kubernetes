package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"strings"

	admissionv1 "k8s.io/api/admission/v1"
)

type patchOperation struct {
	Op    string      `json:"op"`
	Path  string      `json:"path"`
	Value interface{} `json:"value,omitempty"`
}

var (
	certFile     string
	keyFile      string
	port         int
	samplingRate float64
)

func generateRandomHex(n int) string {
	bytes := make([]byte, n)
	if _, err := rand.Read(bytes); err != nil {
		return strings.Repeat("0", n*2)
	}
	return hex.EncodeToString(bytes)
}

func generateTraceparent(sampled bool) string {
	traceID := generateRandomHex(16) // 32 hex chars
	spanID := generateRandomHex(8)   // 16 hex chars
	flag := "00"
	if sampled {
		flag = "01"
	}
	return fmt.Sprintf("00-%s-%s-%s", traceID, spanID, flag)
}

func isSampled(rate float64) bool {
	if rate >= 1.0 {
		return true
	}
	if rate <= 0.0 {
		return false
	}
	n, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return true
	}
	return float64(n.Int64())/10000.0 < rate
}

func mutateHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "invalid method", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		log.Printf("Error reading request body: %v", err)
		http.Error(w, "could not read body", http.StatusBadRequest)
		return
	}

	admReview := admissionv1.AdmissionReview{}
	if err := json.Unmarshal(body, &admReview); err != nil {
		log.Printf("Error unmarshaling AdmissionReview: %v", err)
		http.Error(w, "could not parse admission review", http.StatusBadRequest)
		return
	}

	req := admReview.Request
	if req == nil {
		log.Printf("AdmissionReview has nil Request")
		http.Error(w, "nil request", http.StatusBadRequest)
		return
	}

	resp := admissionv1.AdmissionResponse{
		UID:     req.UID,
		Allowed: true,
	}

	// 1. SPEC-ONLY CHECK: Explicitly ignore status subresource updates
	if req.SubResource == "status" {
		log.Printf("[MUTATE] Ignoring /status subresource update for %s/%s (%s)", req.Namespace, req.Name, req.Kind.Kind)
		admReview.Response = &resp
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(admReview)
		return
	}

	// 2. Extract or Generate Traceparent
	httpTraceparent := r.Header.Get("traceparent")
	sampled := isSampled(samplingRate)

	var targetTraceparent string
	if httpTraceparent != "" && strings.HasPrefix(httpTraceparent, "00-") && len(httpTraceparent) >= 55 {
		targetTraceparent = httpTraceparent
		log.Printf("[MUTATE] Inherited incoming HTTP traceparent header: %s", targetTraceparent)
	} else if sampled {
		targetTraceparent = generateTraceparent(true)
		log.Printf("[MUTATE] Generated fresh W3C traceparent (sampling=%.2f): %s", samplingRate, targetTraceparent)
	}

	if targetTraceparent != "" {
		// Inspect raw object to check existing annotations
		var rawObj struct {
			Metadata struct {
				Annotations map[string]string `json:"annotations,omitempty"`
			} `json:"metadata"`
			Spec struct {
				Template struct {
					Metadata struct {
						Annotations map[string]string `json:"annotations,omitempty"`
					} `json:"metadata"`
				} `json:"template"`
			} `json:"spec"`
		}
		if err := json.Unmarshal(req.Object.Raw, &rawObj); err == nil {
			var patches []patchOperation

			// Check metadata.annotations
			existingTP, hasTP := rawObj.Metadata.Annotations["traceparent"]
			if !hasTP || (req.Operation == admissionv1.Create && existingTP == "") {
				if rawObj.Metadata.Annotations == nil {
					patches = append(patches, patchOperation{
						Op:   "add",
						Path: "/metadata/annotations",
						Value: map[string]string{
							"traceparent": targetTraceparent,
						},
					})
				} else {
					patches = append(patches, patchOperation{
						Op:    "add",
						Path:  "/metadata/annotations/traceparent",
						Value: targetTraceparent,
					})
				}
			}

			// If it's a Deployment or ReplicaSet, also bundle into pod template spec
			if req.Kind.Kind == "Deployment" || req.Kind.Kind == "ReplicaSet" {
				if rawObj.Spec.Template.Metadata.Annotations == nil {
					patches = append(patches, patchOperation{
						Op:   "add",
						Path: "/spec/template/metadata/annotations",
						Value: map[string]string{
							"traceparent": targetTraceparent,
						},
					})
				} else if _, hasTplTP := rawObj.Spec.Template.Metadata.Annotations["traceparent"]; !hasTplTP {
					patches = append(patches, patchOperation{
						Op:    "add",
						Path:  "/spec/template/metadata/annotations/traceparent",
						Value: targetTraceparent,
					})
				}
			}

			if len(patches) > 0 {
				patchBytes, err := json.Marshal(patches)
				if err == nil {
					patchType := admissionv1.PatchTypeJSONPatch
					resp.PatchType = &patchType
					resp.Patch = patchBytes
					log.Printf("[MUTATE] Applied %d patch(es) to %s/%s (%s): %s", len(patches), req.Namespace, req.Name, req.Kind.Kind, string(patchBytes))
				}
			}
		}
	}

	admReview.Response = &resp
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(admReview); err != nil {
		log.Printf("Error encoding response: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}

func main() {
	flag.StringVar(&certFile, "tls-cert-file", "/etc/webhook/certs/tls.crt", "TLS certificate file")
	flag.StringVar(&keyFile, "tls-key-file", "/etc/webhook/certs/tls.key", "TLS private key file")
	flag.IntVar(&port, "port", 8443, "Server port")
	flag.Float64Var(&samplingRate, "sampling-rate", 1.0, "Sampling rate (0.0 to 1.0)")
	flag.Parse()

	log.Printf("Starting Tracing Mutating Admission Webhook on port %d (sampling=%.2f)...", port, samplingRate)

	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", mutateHandler)
	mux.HandleFunc("/healthz", healthHandler)

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	if err := server.ListenAndServeTLS(certFile, keyFile); err != nil {
		log.Fatalf("Server failed: %v", err)
	}
}
