package room

import "github.com/gin-gonic/gin"

func main() {
	r := gin.Default()

	r.GET("/room/health", checkHealth)

	r.Run(":8096")
}

func checkHealth(g *gin.Context) {
	// Check the micro-service health
}
