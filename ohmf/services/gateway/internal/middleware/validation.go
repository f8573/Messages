package middleware

import (
	"bytes"
	"io"
	"net/http"

	"encoding/json"
	"github.com/santhosh-tekuri/jsonschema/v5"
	"io/ioutil"

	_ "embed"
)

//go:embed schemas/message-ingress.schema.json
var messageIngressSchemaBytes []byte

//go:embed schemas/send-message-request.schema.json
var sendMessageRequestSchemaBytes []byte

//go:embed schemas/send-phone-message-request.schema.json
var sendPhoneMessageRequestSchemaBytes []byte

//go:embed schemas/ws-subscribe.schema.json
var wsSubscribeSchemaBytes []byte

//go:embed schemas/ws-send_message.schema.json
var wsSendMessageSchemaBytes []byte

var compiledSchemas = map[string]*jsonschema.Schema{}

func init() {
	// compile known schemas at init
	compiler := jsonschema.NewCompiler()
	if err := compiler.AddResource("message-ingress.json", bytes.NewReader(messageIngressSchemaBytes)); err == nil {
		if s, err := compiler.Compile("message-ingress.json"); err == nil {
			compiledSchemas["message-ingress"] = s
		}
	}
	if err := compiler.AddResource("send-message-request.json", bytes.NewReader(sendMessageRequestSchemaBytes)); err == nil {
		if s, err := compiler.Compile("send-message-request.json"); err == nil {
			compiledSchemas["send-message-request"] = s
		}
	}
	if err := compiler.AddResource("send-phone-message-request.json", bytes.NewReader(sendPhoneMessageRequestSchemaBytes)); err == nil {
		if s, err := compiler.Compile("send-phone-message-request.json"); err == nil {
			compiledSchemas["send-phone-message-request"] = s
		}
	}
	if err := compiler.AddResource("ws-subscribe.json", bytes.NewReader(wsSubscribeSchemaBytes)); err == nil {
		if s, err := compiler.Compile("ws-subscribe.json"); err == nil {
			compiledSchemas["ws-subscribe"] = s
		}
	}
	if err := compiler.AddResource("ws-send_message.json", bytes.NewReader(wsSendMessageSchemaBytes)); err == nil {
		if s, err := compiler.Compile("ws-send_message.json"); err == nil {
			compiledSchemas["ws-send_message"] = s
		}
	}
}

// ValidateJSONMiddleware validates request JSON body against a compiled schema name.
func ValidateJSONMiddleware(schemaName string) func(http.Handler) http.Handler {
	schema, ok := compiledSchemas[schemaName]
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !ok || schema == nil {
				// no schema available, allow through
				next.ServeHTTP(w, r)
				return
			}
			if r.Body == nil {
				http.Error(w, "missing body", http.StatusBadRequest)
				return
			}
			bodyBytes, err := ioutil.ReadAll(r.Body)
			if err != nil {
				http.Error(w, "unable to read body", http.StatusBadRequest)
				return
			}
			// restore body for downstream handlers
			r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

			var data interface{}
			if err := json.Unmarshal(bodyBytes, &data); err == nil {
				if err := schema.Validate(data); err != nil {
					http.Error(w, "invalid request: "+err.Error(), http.StatusBadRequest)
					return
				}
			} else {
				// invalid JSON
				http.Error(w, "invalid json: "+err.Error(), http.StatusBadRequest)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// ValidateData validates a decoded JSON-like value against a compiled schema.
func ValidateData(schemaName string, data interface{}) error {
	schema, ok := compiledSchemas[schemaName]
	if !ok || schema == nil {
		return nil
	}
	return schema.Validate(data)
}
