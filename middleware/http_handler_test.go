package middleware_test

import (
	"bytes"
	"log"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"

	zipkin "github.com/openzipkin/zipkin-go"
	"github.com/openzipkin/zipkin-go/middleware"
	"github.com/openzipkin/zipkin-go/model"
)

var (
	lep, _ = zipkin.NewEndpoint("testSvc", "127.0.0.1:0")
)

type reporterRecorder struct {
	mtx   sync.Mutex
	spans []model.SpanModel
}

func (r *reporterRecorder) Send(span model.SpanModel) {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	r.spans = append(r.spans, span)
}

func (r *reporterRecorder) Flush() []model.SpanModel {
	r.mtx.Lock()
	defer r.mtx.Unlock()

	spans := r.spans
	r.spans = nil
	return spans
}

func (r *reporterRecorder) Close() error {
	_ = r.Flush
	return nil
}

func httpHandler(code int, headers http.Header, body *bytes.Buffer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(code)
		for key, value := range headers {
			log.Println(key, value)
			w.Header().Add(key, value[0])
		}
		w.Write(body.Bytes())
	}
}

func TestHTTPHandlerWrapping(t *testing.T) {
	var (
		spanRecorder = &reporterRecorder{}
		tr, _        = zipkin.NewTracer(spanRecorder, zipkin.WithLocalEndpoint(lep))
		httpRecorder = httptest.NewRecorder()
		requestBuf   = bytes.NewBuffer([]byte("incoming data"))
		responseBuf  = bytes.NewBuffer([]byte("oh oh we have a 404"))
		headers      = make(http.Header)
		code         = 404
	)
	headers.Add("some-key", "some-value")
	headers.Add("other-key", "other-value")

	request, err := http.NewRequest("POST", "/test", requestBuf)
	if err != nil {
		t.Fatalf("unable to create request")
	}

	httpHandlerFunc := http.HandlerFunc(httpHandler(code, headers, responseBuf))

	handler := middleware.WrapHTTPHandler(tr, "wrapper_test", httpHandlerFunc)

	handler.ServeHTTP(httpRecorder, request)

	spans := spanRecorder.Flush()

	if want, have := 1, len(spans); want != have {
		t.Errorf("Expected %d spans, got %d", want, have)
	}

	span := spans[0]

	if want, have := strconv.Itoa(requestBuf.Len()), span.Tags["http.request.size"]; want != have {
		t.Errorf("Expected span request size %s, got %s", want, have)
	}

	if want, have := strconv.Itoa(responseBuf.Len()), span.Tags["http.response.size"]; want != have {
		t.Errorf("Expected span response size %s, got %s", want, have)

	}

	if want, have := strconv.Itoa(code), span.Tags["http.status_code"]; want != have {
		t.Errorf("Expected span status code %s, got %s", want, have)
	}

	if want, have := strconv.Itoa(code), span.Tags["error"]; want != have {
		t.Errorf("Expected span error %q, got %q", want, have)
	}

	if want, have := len(headers), len(httpRecorder.HeaderMap); want != have {
		t.Errorf("Expected http header count %d, got %d", want, have)
	}

	if want, have := code, httpRecorder.Code; want != have {
		t.Errorf("Expected http status code %d, got %d", want, have)
	}

	for key, value := range headers {
		if want, have := value, httpRecorder.HeaderMap.Get(key); want[0] != have {
			t.Errorf("Expected header %s value %s, got %s", key, want, have)
		}
	}

	if want, have := responseBuf.String(), httpRecorder.Body.String(); want != have {
		t.Errorf("Expected body value %q, got %q", want, have)
	}
}
