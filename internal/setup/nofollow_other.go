//go:build !unix

package setup

// NoFollow is 0 where the platform has no O_NOFOLLOW; callers check with
// os.Lstat before opening (RegularOrAbsent).
const NoFollow = 0
