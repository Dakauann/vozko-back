package main

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"vozko/domain/category"
	"vozko/infra/database"
	"vozko/infra/database/schema"

	"gorm.io/gorm"
)

const (
	taxonomyURL = "https://www.google.com/basepages/producttype/taxonomy-with-ids.pt-BR.txt"
	batchSize   = 100
)

type CategoryLine struct {
	ID       string
	FullPath string
	Parts    []string
}

type CategoryInfo struct {
	ID       string
	Name     string
	ParentID *string
	Slug     string
}

func main() {
	log.Println("Starting Google Product Taxonomy Seeder...")

	db, err := database.NewGormDatabase()
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	sqlDB, _ := db.DB()
	defer sqlDB.Close()

	log.Println("Migrating category table to use TEXT IDs...")
	if err := migrateToTextIDs(db); err != nil {
		log.Fatalf("Failed to migrate category table: %v", err)
	}

	log.Println("Fetching Google Product Taxonomy...")
	lines, err := fetchTaxonomy()
	if err != nil {
		log.Fatalf("Failed to fetch taxonomy: %v", err)
	}
	log.Printf("Fetched %d lines from taxonomy file", len(lines))

	categories := parseTaxonomy(lines)
	log.Printf("Parsed %d category entries", len(categories))

	if err := seedCategories(db, categories); err != nil {
		log.Fatalf("Failed to seed categories: %v", err)
	}

	log.Println("Google Product Taxonomy seeding completed successfully!")
}

func migrateToTextIDs(db *gorm.DB) error {
	var tableName string
	if err := db.Raw("SELECT table_name FROM information_schema.tables WHERE table_name = 'categories' AND table_schema = CURRENT_SCHEMA()").Scan(&tableName).Error; err != nil {
		return fmt.Errorf("failed to check if table exists: %w", err)
	}

	if tableName == "" {
		log.Println("Categories table does not exist, creating with TEXT IDs...")
		if err := db.AutoMigrate(&schema.Category{}); err != nil {
			return fmt.Errorf("failed to create categories table: %w", err)
		}
		return nil
	}

	var dataType string
	if err := db.Raw(`
		SELECT data_type 
		FROM information_schema.columns 
		WHERE table_name = 'categories' 
		AND column_name = 'id' 
		AND table_schema = CURRENT_SCHEMA()
	`).Scan(&dataType).Error; err != nil {
		return fmt.Errorf("failed to get column data type: %w", err)
	}

	if dataType == "text" || dataType == "character varying" {
		log.Println("Category IDs are already TEXT type, skipping migration")
		return nil
	}

	log.Printf("Current ID column type: %s, migrating to TEXT...", dataType)

	return db.Transaction(func(tx *gorm.DB) error {
		var constraints []struct {
			ConstraintName string
			TableName      string
		}
		if err := tx.Raw(`
			SELECT tc.constraint_name, tc.table_name
			FROM information_schema.table_constraints tc
			JOIN information_schema.constraint_column_usage ccu ON ccu.constraint_name = tc.constraint_name
			WHERE tc.constraint_type = 'FOREIGN KEY'
			AND (ccu.table_name = 'categories' OR tc.table_name = 'categories')
			AND tc.table_schema = CURRENT_SCHEMA()
		`).Scan(&constraints).Error; err != nil {
			log.Printf("Warning: failed to get FK constraints: %v", err)
		}

		for _, c := range constraints {
			log.Printf("Dropping constraint %s on table %s", c.ConstraintName, c.TableName)
			if err := tx.Exec(fmt.Sprintf("ALTER TABLE %s DROP CONSTRAINT IF EXISTS %s", c.TableName, c.ConstraintName)).Error; err != nil {
				log.Printf("Warning: failed to drop constraint %s: %v", c.ConstraintName, err)
			}
		}

		if err := tx.Exec("ALTER TABLE categories ALTER COLUMN id TYPE TEXT").Error; err != nil {
			return fmt.Errorf("failed to alter id column: %w", err)
		}
		log.Println("Changed id column to TEXT")

		if err := tx.Exec("ALTER TABLE categories ALTER COLUMN parent_id TYPE TEXT").Error; err != nil {
			return fmt.Errorf("failed to alter parent_id column: %w", err)
		}
		log.Println("Changed parent_id column to TEXT")

		var variantsCatCol string
		if err := tx.Raw(`
			SELECT column_name FROM information_schema.columns 
			WHERE table_name = 'variants' AND column_name = 'category_id' AND table_schema = CURRENT_SCHEMA()
		`).Scan(&variantsCatCol).Error; err == nil && variantsCatCol != "" {
			if err := tx.Exec("ALTER TABLE variants ALTER COLUMN category_id TYPE TEXT").Error; err != nil {
				log.Printf("Warning: failed to alter variants.category_id: %v", err)
			} else {
				log.Println("Changed variants.category_id column to TEXT")
			}
		}

		var propertiesCatCol string
		if err := tx.Raw(`
			SELECT column_name FROM information_schema.columns 
			WHERE table_name = 'properties' AND column_name = 'category_id' AND table_schema = CURRENT_SCHEMA()
		`).Scan(&propertiesCatCol).Error; err == nil && propertiesCatCol != "" {
			if err := tx.Exec("ALTER TABLE properties ALTER COLUMN category_id TYPE TEXT").Error; err != nil {
				log.Printf("Warning: failed to alter properties.category_id: %v", err)
			} else {
				log.Println("Changed properties.category_id column to TEXT")
			}
		}

		if err := tx.Exec(`
			ALTER TABLE categories 
			ADD CONSTRAINT fk_categories_parent 
			FOREIGN KEY (parent_id) REFERENCES categories(id) ON DELETE SET NULL
		`).Error; err != nil {
			log.Printf("Warning: failed to re-add parent FK: %v", err)
		}

		log.Println("Migration to TEXT IDs completed successfully")
		return nil
	})
}

func fetchTaxonomy() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", taxonomyURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch taxonomy: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		lines = append(lines, line)
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan lines: %w", err)
	}

	return lines, nil
}

func parseTaxonomy(lines []string) []CategoryLine {
	var categories []CategoryLine

	for _, line := range lines {
		parts := strings.SplitN(line, " - ", 2)
		if len(parts) != 2 {
			log.Printf("Skipping malformed line: %s", line)
			continue
		}

		idStr := strings.TrimSpace(parts[0])
		if _, err := strconv.ParseInt(idStr, 10, 64); err != nil {
			log.Printf("Skipping line with invalid ID: %s", line)
			continue
		}

		fullPath := strings.TrimSpace(parts[1])
		categoryParts := strings.Split(fullPath, " > ")

		for i := range categoryParts {
			categoryParts[i] = strings.TrimSpace(categoryParts[i])
		}

		categories = append(categories, CategoryLine{
			ID:       idStr,
			FullPath: fullPath,
			Parts:    categoryParts,
		})
	}

	return categories
}

func seedCategories(db *gorm.DB, categories []CategoryLine) error {

	pathToCategory := make(map[string]*CategoryInfo)

	idToPath := make(map[string]string)

	for _, cat := range categories {
		fullPath := strings.Join(cat.Parts, " > ")
		idToPath[cat.ID] = fullPath
		pathToCategory[fullPath] = &CategoryInfo{
			ID:   cat.ID,
			Name: cat.Parts[len(cat.Parts)-1],
			Slug: category.NormalizeSlug(cat.Parts[len(cat.Parts)-1]),
		}

		for i := 1; i < len(cat.Parts); i++ {
			parentPath := strings.Join(cat.Parts[:i], " > ")
			if _, exists := pathToCategory[parentPath]; !exists {
				intermediateID := generateDeterministicID(parentPath)
				pathToCategory[parentPath] = &CategoryInfo{
					ID:   intermediateID,
					Name: cat.Parts[i-1],
					Slug: category.NormalizeSlug(cat.Parts[i-1]),
				}
			}
		}
	}

	for path, catInfo := range pathToCategory {
		parts := strings.Split(path, " > ")
		if len(parts) > 1 {
			parentPath := strings.Join(parts[:len(parts)-1], " > ")
			if parentCat, exists := pathToCategory[parentPath]; exists {
				parentID := parentCat.ID
				catInfo.ParentID = &parentID
			}
		}
	}

	slugCounts := make(map[string]int)
	for _, catInfo := range pathToCategory {
		baseSlug := catInfo.Slug
		count := slugCounts[baseSlug]
		if count > 0 {
			catInfo.Slug = fmt.Sprintf("%s-%d", baseSlug, count)
		}
		slugCounts[baseSlug] = count + 1
	}

	existingIDs := make(map[string]bool)
	existingSlugs := make(map[string]bool)
	var existing []schema.Category
	if err := db.Select("id, slug").Find(&existing).Error; err != nil {
		return fmt.Errorf("failed to query existing categories: %w", err)
	}
	for _, cat := range existing {
		existingIDs[cat.ID] = true
		existingSlugs[cat.Slug] = true
	}
	log.Printf("Found %d existing categories in database", len(existing))

	var toInsert []schema.Category
	for _, catInfo := range pathToCategory {
		if existingIDs[catInfo.ID] {
			continue
		}

		finalSlug := catInfo.Slug
		counter := 1
		for existingSlugs[finalSlug] {
			finalSlug = fmt.Sprintf("%s-%d", catInfo.Slug, counter)
			counter++
		}
		existingSlugs[finalSlug] = true

		toInsert = append(toInsert, schema.Category{
			ID:       catInfo.ID,
			Name:     catInfo.Name,
			Slug:     finalSlug,
			ParentID: catInfo.ParentID,
		})
	}

	log.Printf("Inserting %d new categories (skipped %d existing)",
		len(toInsert), len(pathToCategory)-len(toInsert))

	sortedCategories := sortByHierarchy(toInsert)

	for i := 0; i < len(sortedCategories); i += batchSize {
		end := i + batchSize
		if end > len(sortedCategories) {
			end = len(sortedCategories)
		}
		batch := sortedCategories[i:end]

		if err := db.CreateInBatches(batch, batchSize).Error; err != nil {
			for _, cat := range batch {
				if err := db.Create(&cat).Error; err != nil {
					if strings.Contains(err.Error(), "duplicate") ||
						strings.Contains(err.Error(), "UNIQUE constraint") {
						log.Printf("Skipping duplicate category: %s (%s)", cat.Name, cat.ID)
						continue
					}
					return fmt.Errorf("failed to insert category %s: %w", cat.Name, err)
				}
			}
		}

		log.Printf("Inserted batch %d-%d of %d", i+1, end, len(sortedCategories))
	}

	return nil
}

func generateDeterministicID(path string) string {
	hash := fnv1a(path)
	return fmt.Sprintf("auto_%d", hash)
}

func fnv1a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	hash := uint64(offset64)
	for i := 0; i < len(s); i++ {
		hash ^= uint64(s[i])
		hash *= prime64
	}
	return hash
}

func sortByHierarchy(categories []schema.Category) []schema.Category {
	catMap := make(map[string]*schema.Category)
	for i := range categories {
		catMap[categories[i].ID] = &categories[i]
	}

	depths := make(map[string]int)
	var getDepth func(id string) int
	getDepth = func(id string) int {
		if d, ok := depths[id]; ok {
			return d
		}
		cat, exists := catMap[id]
		if !exists || cat.ParentID == nil {
			depths[id] = 0
			return 0
		}
		depths[id] = getDepth(*cat.ParentID) + 1
		return depths[id]
	}

	for _, cat := range categories {
		getDepth(cat.ID)
	}

	result := make([]schema.Category, len(categories))
	copy(result, categories)

	for i := 0; i < len(result)-1; i++ {
		for j := 0; j < len(result)-i-1; j++ {
			if depths[result[j].ID] > depths[result[j+1].ID] {
				result[j], result[j+1] = result[j+1], result[j]
			}
		}
	}

	return result
}

func init() {
	if os.Getenv("DATABASE_URL") == "" {
		loadEnvFile(".env")
	}
}

func loadEnvFile(filename string) {
	file, err := os.Open(filename)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			value = strings.Trim(value, `"'`)
			if os.Getenv(key) == "" {
				os.Setenv(key, value)
			}
		}
	}
}
