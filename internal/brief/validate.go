package brief

import "github.com/seanmcmahon101/AgentBrief/internal/model"

// Thin aliases so render code reads naturally; the rules live in model so the
// detector's normalisation pass applies them to every output, JSON included.
func validVersion(v string) bool { return model.ValidVersion(v) }
func validPath(p string) bool    { return model.ValidPath(p) }
func validPattern(p string) bool { return model.ValidPattern(p) }
func validPkg(p string) bool     { return model.ValidPkg(p) }
func validName(p string) bool    { return model.ValidName(p) }
func validReason(p string) bool  { return model.ValidReason(p) }
