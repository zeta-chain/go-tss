package conversion

import (
	"testing"

	"github.com/magiconair/properties/assert"
)

func TestVersionCheck(t *testing.T) {
	expectedVer := "1.4.0"
	currentVer := "1.3.0"
	ret, err := VersionLTCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, true)

	expectedVerErr := "none"
	_, err = VersionLTCheck(currentVer, expectedVerErr)
	assert.Equal(t, err.Error(), "fail to parse the expected version")
	currentVer = "abc"
	_, err = VersionLTCheck(currentVer, expectedVer)
	assert.Equal(t, err.Error(), "fail to parse the current version")

	expectedVer = "1.2.0"
	currentVer = "1.3.0"
	ret, err = VersionLTCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, false)

	currentVer = "1.2.2"
	expectedVer = "1.2.0"
	ret, err = VersionLTCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, false)

	expectedVer = "0.14.0"
	currentVer = "0.13.9"
	ret, err = VersionLTCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, true)

	expectedVer = "0.14.0"
	currentVer = "0.14.0"
	ret, err = VersionLTCheck(currentVer, expectedVer)
	assert.Equal(t, err, nil)
	assert.Equal(t, ret, false)
}
