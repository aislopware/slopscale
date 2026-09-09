package hscontrol

import (
	"errors"
	"net/http"

	"github.com/aislopware/slopscale/hscontrol/state"
	"github.com/aislopware/slopscale/hscontrol/types"
	"github.com/aislopware/slopscale/hscontrol/wire"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
)

// The tailnet lock endpoints the client calls over the control
// connection, /machine/tka/*; see docs/ref/tailnet-lock.md and
// ipn/ipnlocal/tailnet-lock.go in the client. The client sends them as
// GET with a JSON body, so the routes take any method. Every request
// names the node key, which must be the session's.

// tkaRoutes mounts the endpoints under /machine/tka.
func (ns *noiseServer) tkaRoutes(r chi.Router) {
	r.HandleFunc("/init/begin", ns.TKAInitBeginHandler)
	r.HandleFunc("/init/finish", ns.TKAInitFinishHandler)
	r.HandleFunc("/bootstrap", ns.TKABootstrapHandler)
	r.HandleFunc("/sync/offer", ns.TKASyncOfferHandler)
	r.HandleFunc("/sync/send", ns.TKASyncSendHandler)
	r.HandleFunc("/disable", ns.TKADisableHandler)
	r.HandleFunc("/sign", ns.TKASignHandler)
	r.HandleFunc("/affected-sigs", ns.TKAAffectedSigsHandler)
}

// tkaRequest decodes the request body into req and resolves the node
// the session's machine key owns for nodeKey; it writes the error and
// returns false when either fails.
func (ns *noiseServer) tkaRequest(
	writer http.ResponseWriter,
	r *http.Request,
	req any,
	nodeKey func() key.NodePublic,
) (types.NodeView, bool) {
	err := wire.UnmarshalRead(r.Body, req)
	if err != nil {
		httpError(writer, NewHTTPError(http.StatusBadRequest, "invalid tailnet lock request", err))

		return types.NodeView{}, false
	}

	node, err := ns.sessionNode(nodeKey())
	if err != nil {
		httpError(writer, err)

		return types.NodeView{}, false
	}

	return node, true
}

// tkaError writes a tailnet lock failure with the status its cause
// calls for.
func tkaError(writer http.ResponseWriter, msg string, err error) {
	code := http.StatusInternalServerError

	switch {
	case errors.Is(err, types.ErrTailnetLockEnabled), errors.Is(err, types.ErrTailnetLockDisabled),
		errors.Is(err, types.ErrTailnetLockNoPendingInit):
		code = http.StatusConflict
	case errors.Is(err, types.ErrTailnetLockNotAdmin), errors.Is(err, types.ErrTailnetLockBadSecret):
		code = http.StatusForbidden
	case errors.Is(err, state.ErrTailnetLockInvalid), errors.Is(err, types.ErrTailnetLockMissingSignature),
		errors.Is(err, types.ErrTailnetLockBadSignature):
		code = http.StatusBadRequest
	case errors.Is(err, state.ErrNodeNotFound):
		code = http.StatusNotFound
	}

	httpError(writer, NewHTTPError(code, msg+": "+err.Error(), err))
}

func tkaRespond(writer http.ResponseWriter, resp any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")

	err := wire.MarshalWrite(writer, resp)
	if err != nil {
		log.Error().Caller().Err(err).Msg("encoding the tailnet lock response")
	}
}

// TKAInitBeginHandler takes the genesis `tailscale lock init` proposes
// and lists the nodes to sign ([tailcfg.TKAInitBeginRequest]).
func (ns *noiseServer) TKAInitBeginHandler(writer http.ResponseWriter, r *http.Request) {
	var req tailcfg.TKAInitBeginRequest

	node, ok := ns.tkaRequest(writer, r, &req, func() key.NodePublic { return req.NodeKey })
	if !ok {
		return
	}

	need, err := ns.slopscale.state.TailnetLockInitBegin(node, req.GenesisAUM)
	if err != nil {
		tkaError(writer, "tailnet lock init", err)

		return
	}

	tkaRespond(writer, &tailcfg.TKAInitBeginResponse{NeedSignatures: need})
}

// TKAInitFinishHandler switches the lock on with the signatures
// `tailscale lock init` made ([tailcfg.TKAInitFinishRequest]).
func (ns *noiseServer) TKAInitFinishHandler(writer http.ResponseWriter, r *http.Request) {
	var req tailcfg.TKAInitFinishRequest

	node, ok := ns.tkaRequest(writer, r, &req, func() key.NodePublic { return req.NodeKey })
	if !ok {
		return
	}

	c, err := ns.slopscale.state.TailnetLockInitFinish(node, req.Signatures, req.SupportDisablement)
	if err != nil {
		tkaError(writer, "tailnet lock init", err)

		return
	}

	ns.slopscale.Change(c)
	tkaRespond(writer, &tailcfg.TKAInitFinishResponse{})
}

// TKABootstrapHandler hands a node the genesis, or the disablement
// secret once the lock is off ([tailcfg.TKABootstrapRequest]).
func (ns *noiseServer) TKABootstrapHandler(writer http.ResponseWriter, r *http.Request) {
	var req tailcfg.TKABootstrapRequest

	_, ok := ns.tkaRequest(writer, r, &req, func() key.NodePublic { return req.NodeKey })
	if !ok {
		return
	}

	resp, err := ns.slopscale.state.TailnetLockBootstrap()
	if err != nil {
		tkaError(writer, "tailnet lock bootstrap", err)

		return
	}

	tkaRespond(writer, &resp)
}

// TKASyncOfferHandler answers a node's sync offer with the server's and
// the AUMs the node is missing ([tailcfg.TKASyncOfferRequest]).
func (ns *noiseServer) TKASyncOfferHandler(writer http.ResponseWriter, r *http.Request) {
	var req tailcfg.TKASyncOfferRequest

	_, ok := ns.tkaRequest(writer, r, &req, func() key.NodePublic { return req.NodeKey })
	if !ok {
		return
	}

	resp, err := ns.slopscale.state.TailnetLockSyncOffer(req.Head, req.Ancestors)
	if err != nil {
		tkaError(writer, "tailnet lock sync", err)

		return
	}

	tkaRespond(writer, &resp)
}

// TKASyncSendHandler takes the AUMs a node believes the server is
// missing ([tailcfg.TKASyncSendRequest]).
func (ns *noiseServer) TKASyncSendHandler(writer http.ResponseWriter, r *http.Request) {
	var req tailcfg.TKASyncSendRequest

	_, ok := ns.tkaRequest(writer, r, &req, func() key.NodePublic { return req.NodeKey })
	if !ok {
		return
	}

	head, c, err := ns.slopscale.state.TailnetLockSyncSend(req.MissingAUMs)
	if err != nil {
		tkaError(writer, "tailnet lock sync", err)

		return
	}

	ns.slopscale.Change(c)
	tkaRespond(writer, &tailcfg.TKASyncSendResponse{Head: head})
}

// TKADisableHandler switches the lock off with a disablement secret
// ([tailcfg.TKADisableRequest]), as `tailscale lock disable` does.
func (ns *noiseServer) TKADisableHandler(writer http.ResponseWriter, r *http.Request) {
	var req tailcfg.TKADisableRequest

	_, ok := ns.tkaRequest(writer, r, &req, func() key.NodePublic { return req.NodeKey })
	if !ok {
		return
	}

	c, err := ns.slopscale.state.TailnetLockDisable(req.DisablementSecret)
	if err != nil {
		tkaError(writer, "tailnet lock disable", err)

		return
	}

	ns.slopscale.Change(c)
	tkaRespond(writer, &tailcfg.TKADisableResponse{})
}

// TKASignHandler records a node key signature a signing node made
// ([tailcfg.TKASubmitSignatureRequest]), as `tailscale lock sign` does.
func (ns *noiseServer) TKASignHandler(writer http.ResponseWriter, r *http.Request) {
	var req tailcfg.TKASubmitSignatureRequest

	_, ok := ns.tkaRequest(writer, r, &req, func() key.NodePublic { return req.NodeKey })
	if !ok {
		return
	}

	_, c, err := ns.slopscale.state.TailnetLockSubmitSignature(req.Signature)
	if err != nil {
		tkaError(writer, "tailnet lock sign", err)

		return
	}

	ns.slopscale.Change(c)
	tkaRespond(writer, &tailcfg.TKASubmitSignatureResponse{})
}

// TKAAffectedSigsHandler lists the node key signatures a signing key
// made ([tailcfg.TKASignaturesUsingKeyRequest]), which `tailscale lock
// remove` re-signs before dropping the key.
func (ns *noiseServer) TKAAffectedSigsHandler(writer http.ResponseWriter, r *http.Request) {
	var req tailcfg.TKASignaturesUsingKeyRequest

	_, ok := ns.tkaRequest(writer, r, &req, func() key.NodePublic { return req.NodeKey })
	if !ok {
		return
	}

	tkaRespond(writer, &tailcfg.TKASignaturesUsingKeyResponse{
		Signatures: ns.slopscale.state.TailnetLockAffectedSignatures(req.KeyID),
	})
}
