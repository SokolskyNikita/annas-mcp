package anna

import (
	"os"

	"github.com/joho/godotenv"
)

// liveSetting reads a setting from the environment, then the repository .env.
func liveSetting(name string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	dotenv, _ := godotenv.Read("../.env")
	return dotenv[name]
}
