package conversion

import (
	"testing"

	"github.com/magiconair/properties/assert"
)

func TestVersionCheck(t *testing.T) {
	expectedVer := "<= 1.2.3, >= 1.4"
	currentVer := "1.3.0"

	ret, err := VersionCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, false)

	expectedVerErr := "none"
	_, err = VersionCheck(currentVer, expectedVerErr)
	assert.Equal(t, err.Error(), "fail to parse the expected version")
	currentVer = "abc"
	_, err = VersionCheck(currentVer, expectedVer)
	assert.Equal(t, err.Error(), "fail to parse the current version")

	expectedVer = ">1.2"
	currentVer = "1.3"
	ret, err = VersionCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, true)

	currentVer = "1.2.2"
	ret, err = VersionCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, true)

	currentVer = "1.2"
	ret, err = VersionCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, false)
}
