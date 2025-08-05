package models

import (
	"errors"
	"fmt"

	"gorm.io/gorm"
)

type Image struct {
	gorm.Model
	UUID         string `gorm:"uniqueIndex"`
	WhatIsThere  string
	WhatIsItLike string
	WhereIsIt    string
	Style        string
	Finished     bool
	URL          string
}

// Prompt generates the OpenAI image prompt for this image.
func (i *Image) Prompt() string {
	return fmt.Sprintf("Attached is a reference image of the cartoon tiger Ty. Draw Ty in the following scenario:\n\nWhat is there?\n%s\n\nWhat is it like?\n%s\n\nWhat else is there?\n%s\n\nUse a %s style.", i.WhatIsThere, i.WhatIsItLike, i.WhereIsIt, i.Style)
}

type ImageDAO struct {
	db *gorm.DB
}

func NewImageDAO(db *gorm.DB) *ImageDAO {
	return &ImageDAO{db: db}
}

// Create creates a new image record
func (dao *ImageDAO) Create(image *Image) error {
	return dao.db.Create(image).Error
}

// GetByUUID retrieves an image by its UUID
func (dao *ImageDAO) GetByUUID(uuid string) (*Image, error) {
	var image Image
	err := dao.db.Where("uuid = ?", uuid).First(&image).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &image, nil
}

// GetAll retrieves all images with optional pagination
func (dao *ImageDAO) GetAll(limit, offset int) ([]Image, error) {
	var images []Image
	query := dao.db.Model(&Image{})

	if limit > 0 {
		query = query.Limit(limit)
	}
	if offset > 0 {
		query = query.Offset(offset)
	}

	err := query.Find(&images).Error
	return images, err
}

// Update updates an existing image record
func (dao *ImageDAO) Update(image *Image) error {
	return dao.db.Save(image).Error
}

// UpdateByUUID updates an image by UUID
func (dao *ImageDAO) UpdateByUUID(uuid string, updates map[string]any) error {
	result := dao.db.Model(&Image{}).Where("uuid = ?", uuid).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Delete soft deletes an image by ID
func (dao *ImageDAO) Delete(id uint) error {
	result := dao.db.Delete(&Image{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// DeleteByUUID soft deletes an image by UUID
func (dao *ImageDAO) DeleteByUUID(uuid string) error {
	result := dao.db.Where("uuid = ?", uuid).Delete(&Image{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// HardDelete permanently deletes an image by ID
func (dao *ImageDAO) HardDelete(id uint) error {
	result := dao.db.Unscoped().Delete(&Image{}, id)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// Count returns the total number of images
func (dao *ImageDAO) Count() (int64, error) {
	var count int64
	err := dao.db.Model(&Image{}).Count(&count).Error
	return count, err
}

// Exists checks if an image exists by UUID
func (dao *ImageDAO) Exists(uuid string) (bool, error) {
	var count int64
	err := dao.db.Model(&Image{}).Where("uuid = ?", uuid).Count(&count).Error
	return count > 0, err
}
