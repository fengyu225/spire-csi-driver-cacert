package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	admissionv1 "k8s.io/api/admission/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
)

const (
	tlsCertPath = "/etc/webhook/certs/svid.pem"
	tlsKeyPath  = "/etc/webhook/certs/svid_key.pem"
)

var (
	runtimeScheme = runtime.NewScheme()
	codecs        = serializer.NewCodecFactory(runtimeScheme)
	deserializer  = codecs.UniversalDeserializer()
)

type WebhookServer struct {
	server     *http.Server
	kubeClient *kubernetes.Clientset
	controller *Controller
}

func addCSIVolume(pod *corev1.Pod) (patches []map[string]interface{}) {
	if pod.Spec.Volumes == nil {
		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  "/spec/volumes",
			"value": []interface{}{},
		})
	}

	csiVolume := map[string]interface{}{
		"name": "cacerts",
		"csi": map[string]interface{}{
			"driver":   "cacerts.csi.cert-manager.io",
			"readOnly": true,
			"volumeAttributes": map[string]interface{}{
				"os":                "alpine",
				"caProviderClasses": "ca-provider",
			},
		},
	}

	patches = append(patches, map[string]interface{}{
		"op":    "add",
		"path":  "/spec/volumes/-",
		"value": csiVolume,
	})

	for i := range pod.Spec.Containers {
		volumeMount := map[string]interface{}{
			"name":      "cacerts",
			"mountPath": "/etc/ssl/certs",
			"readOnly":  true,
		}

		if pod.Spec.Containers[i].VolumeMounts == nil {
			patches = append(patches, map[string]interface{}{
				"op":    "add",
				"path":  fmt.Sprintf("/spec/containers/%d/volumeMounts", i),
				"value": []interface{}{},
			})
		}

		patches = append(patches, map[string]interface{}{
			"op":    "add",
			"path":  fmt.Sprintf("/spec/containers/%d/volumeMounts/-", i),
			"value": volumeMount,
		})
	}

	return patches
}

func (whsvr *WebhookServer) mutate(ar *admissionv1.AdmissionReview) *admissionv1.AdmissionResponse {
	req := ar.Request
	log.Printf("Handling admission request for %s/%s", req.Namespace, req.Name)

	pod := &corev1.Pod{}
	if err := json.Unmarshal(req.Object.Raw, pod); err != nil {
		log.Printf("Could not unmarshal raw object: %v", err)
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	if val, ok := pod.Labels["spiffe.io/spire-managed-identity"]; !ok || val != "true" {
		log.Printf("Pod %s/%s doesn't have required label, skipping", req.Namespace, req.Name)
		return &admissionv1.AdmissionResponse{
			Allowed: true,
		}
	}

	patches := addCSIVolume(pod)

	patchBytes, err := json.Marshal(patches)
	if err != nil {
		return &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	}

	log.Printf("Generated patches for pod %s/%s: %s", req.Namespace, req.Name, string(patchBytes))

	return &admissionv1.AdmissionResponse{
		Allowed: true,
		Patch:   patchBytes,
		PatchType: func() *admissionv1.PatchType {
			pt := admissionv1.PatchTypeJSONPatch
			return &pt
		}(),
	}
}

func (whsvr *WebhookServer) serve(w http.ResponseWriter, r *http.Request) {
	var body []byte
	if r.Body != nil {
		if data, err := ioutil.ReadAll(r.Body); err == nil {
			body = data
		}
	}

	if contentType := r.Header.Get("Content-Type"); contentType != "application/json" {
		log.Printf("Content-Type=%s, want application/json", contentType)
		http.Error(w, "invalid Content-Type, want application/json", http.StatusUnsupportedMediaType)
		return
	}

	var admissionResponse *admissionv1.AdmissionResponse
	ar := admissionv1.AdmissionReview{}
	if _, _, err := deserializer.Decode(body, nil, &ar); err != nil {
		log.Printf("Can't decode body: %v", err)
		admissionResponse = &admissionv1.AdmissionResponse{
			Result: &metav1.Status{
				Message: err.Error(),
			},
		}
	} else {
		admissionResponse = whsvr.mutate(&ar)
	}

	response := admissionv1.AdmissionReview{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "admission.k8s.io/v1",
			Kind:       "AdmissionReview",
		},
	}
	if admissionResponse != nil {
		response.Response = admissionResponse
		if ar.Request != nil {
			response.Response.UID = ar.Request.UID
		}
	}

	resp, err := json.Marshal(response)
	if err != nil {
		log.Printf("Can't encode response: %v", err)
		http.Error(w, fmt.Sprintf("could not encode response: %v", err), http.StatusInternalServerError)
		return
	}

	if _, err := w.Write(resp); err != nil {
		log.Printf("Can't write response: %v", err)
		http.Error(w, fmt.Sprintf("could not write response: %v", err), http.StatusInternalServerError)
	}
}

func init() {
	log.SetOutput(os.Stdout)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)
	log.Printf("Initializing webhook server, writing to stdout")
}

func main() {
	log.Printf("1. Starting webhook server initialization")

	log.Printf("2. Creating in-cluster config")
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Fatalf("Failed to get in-cluster config: %v", err)
	}

	log.Printf("3. Creating Kubernetes clientset")
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create Kubernetes client: %v", err)
	}

	log.Printf("4. Creating dynamic client")
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		log.Fatalf("Failed to create dynamic client: %v", err)
	}

	log.Printf("5. Creating namespace informer")
	factory := informers.NewSharedInformerFactory(clientset, time.Hour*24)
	namespaceInformer := factory.Core().V1().Namespaces().Informer()

	log.Printf("6. Creating controller")
	controller := NewController(dynamicClient, namespaceInformer)

	stopCh := make(chan struct{})
	log.Printf("7. Starting controller")
	go controller.Run(stopCh)

	log.Printf("8. Creating webhook server")
	whsvr := &WebhookServer{
		server: &http.Server{
			Addr: ":8443",
		},
		kubeClient: clientset,
		controller: controller,
	}

	log.Printf("9. Setting up HTTP handlers")
	mux := http.NewServeMux()
	mux.HandleFunc("/mutate", whsvr.serve)
	whsvr.server.Handler = mux

	log.Printf("10. Starting webhook server on :8443")
	go func() {
		log.Printf("11. About to start ListenAndServeTLS")
		if err := whsvr.server.ListenAndServeTLS(tlsCertPath, tlsKeyPath); err != nil {
			log.Printf("12. Failed to listen and serve: %v", err)
			os.Exit(1)
		}
	}()

	log.Printf("13. Webhook server started, waiting for shutdown signal")

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGINT, syscall.SIGTERM)
	<-signalChan

	log.Printf("14. Received shutdown signal, gracefully shutting down...")

	close(stopCh)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := whsvr.server.Shutdown(ctx); err != nil {
		log.Printf("Failed to gracefully shutdown: %v", err)
	}
	log.Printf("15. Server shutdown complete")
}
