package proptest

import (
	"github.com/leanovate/gopter"
	"github.com/leanovate/gopter/gen"
)

// AppName generates valid app names.
func AppName() gopter.Gen {
	return gen.Identifier()
}

// ProfileName generates valid profile names.
func ProfileName() gopter.Gen {
	return gen.Identifier()
}
