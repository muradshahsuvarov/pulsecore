package utils

import (
	"github.com/gin-gonic/gin"
)

func main() {
	r := gin.Default()

	r.POST("/utils/transform", transformData)
	r.GET("/utils/health", checkHealth)

	r.Run(":8097")
}

func transformData(g *gin.Context) {

}

func checkHealth(g *gin.Context) {
	// Check the micro-service health
}
