module operation-advertise/backend

go 1.22

require (
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/gorilla/handlers v1.5.2
	github.com/gorilla/mux v1.8.1
	github.com/mattn/go-sqlite3 v1.14.22
	golang.org/x/crypto v0.24.0
)

require github.com/felixge/httpsnoop v1.0.4 // indirect

replace golang.org/x/crypto => github.com/golang/crypto v0.24.0