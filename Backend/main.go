package main

import (
	"context"
	"log"

	"leave-management/internal/config"
	"leave-management/internal/db"
	"leave-management/internal/router"

	"github.com/gin-gonic/gin"
)

func main() {
	cfg := config.Load()
	ctx := context.Background()
	pool := db.NewPool(ctx, cfg.DatabaseURL)
	defer pool.Close()

	r := gin.Default()
	router.Setup(r, pool)

	log.Printf("listening on :%s ...", cfg.Port)
	if err := r.Run(":" + cfg.Port); err != nil {
		log.Fatal(err)
	}
}
