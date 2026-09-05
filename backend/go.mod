module oci-panel

go 1.22

require (
	github.com/gin-gonic/gin v1.9.1
	github.com/golang-jwt/jwt/v5 v5.2.1
	github.com/google/uuid v1.6.0
	github.com/gorilla/websocket v1.5.1
	github.com/oracle/oci-go-sdk/v65 v65.65.0
	github.com/pquerna/otp v1.4.0
	github.com/redis/go-redis/v9 v9.5.1
	golang.org/x/crypto v0.22.0
	gopkg.in/yaml.v3 v3.0.1
	gorm.io/driver/postgres v1.5.7
	gorm.io/gorm v1.25.9
)

replace github.com/rogpeppe/go-internal => github.com/rogpeppe/go-internal v1.13.1

