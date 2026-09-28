package updateflow

import (
	"errors"
	"fmt"
	"net"
)

// ErrUpdaterUnavailable means there is no updater to talk to, as opposed to an
// updater that answered badly.
//
// The progress endpoints used to report both as 502, so every deployment without
// the updater sidecar — anything not run from the Docker Compose file — logged a
// bad gateway on every poll from every open panel, forever. On a live node whose
// configured updater address had no host behind it, each of those also waited
// three seconds for address resolution to give up. A 502 there read as an outage
// during unrelated incidents, when all it meant was that this node cannot update
// itself. ApplyUpdate already reports that condition as updater_unavailable; the
// progress endpoints now do the same.
var ErrUpdaterUnavailable = errors.New("updater is unavailable")

// errAutoUpdateDisabled short-circuits progress requests on a node that has
// auto-update turned off. ApplyUpdate refuses to start a run there, so there is no
// progress to report, and dialling an updater that is usually not deployed on such
// a node only costs a connection attempt per poll.
var errAutoUpdateDisabled = fmt.Errorf("%w: auto update is disabled on this node", ErrUpdaterUnavailable)

// classifyUpdaterError marks failures that happened before any connection to the
// updater was made as ErrUpdaterUnavailable and leaves everything else untouched.
func classifyUpdaterError(err error) error {
	if updaterUnreachable(err) {
		return fmt.Errorf("%w: %w", ErrUpdaterUnavailable, err)
	}
	return err
}

// updaterUnreachable reports whether a request never reached an updater at all:
// nothing listening, no route to the address, a name that does not resolve, or a
// dial that timed out. Name resolution failures surface as dial errors too.
//
// It is false once a connection was made. An updater that accepts the connection
// and then stalls or answers with an error is a real fault and stays a bad gateway.
// So is a request cut off by the client's overall timeout: by then the error no
// longer says which phase was slow, and guessing "unreachable" could hide a hung
// updater.
func updaterUnreachable(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}
