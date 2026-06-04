package profiles

import "errors"

var (
	ErrMissingURL          = errors.New("url is required")
	ErrMissingCredentials  = errors.New("username and password, or api token, are required")
	ErrNameURLRequired     = errors.New("name and url are required")
	ErrAuthRequired        = errors.New("api token or username and password are required")
	ErrNotFound            = errors.New("profile not found")
	ErrDBRequired          = errors.New("DATABASE_URL required for this operation")
)
