package hub

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
)

func pushTransportReason(err error) string {
	if err == nil {
		return ""
	}
	if errors.Is(err, context.Canceled) {
		return "TransportCanceled"
	}
	var network net.Error
	if errors.Is(err, context.DeadlineExceeded) || errors.As(err, &network) && network.Timeout() {
		return "TransportTimeout"
	}
	return "TransportFailure"
}

// Keep only recognized symbolic reasons. Upstream bodies can contain secret
// subscription URLs, so never store or return arbitrary response text.
func pushRejectionReason(response *http.Response) string {
	if response == nil || response.StatusCode < 400 || response.Body == nil {
		return ""
	}
	var body struct {
		Reason string `json:"reason"`
	}
	if json.NewDecoder(io.LimitReader(response.Body, 2048)).Decode(&body) != nil {
		return ""
	}
	switch body.Reason {
	case "BadJwtToken", "VapidPkHashMismatch", "BadVapidPublicKey":
		return body.Reason
	}
	return ""
}
