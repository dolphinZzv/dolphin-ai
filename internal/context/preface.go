package context

import (
	_ "embed"
)

//go:embed PREFACE.md
var DefaultPreface string

//go:embed BUILTIN_SKILLS.md
var BuiltinSkills string

//go:embed SELF_EVOLUTION_SKILLS.md
var SelfEvolutionSkills string
