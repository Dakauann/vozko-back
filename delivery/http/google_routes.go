package http

import (
	googleaccounthttp "vozko/delivery/http/googleaccount"
	googlesheethttp "vozko/delivery/http/googlesheet"
)

type GoogleRoutes struct {
	Accounts *googleaccounthttp.Handler
	Sheets   *googlesheethttp.Handler
}
