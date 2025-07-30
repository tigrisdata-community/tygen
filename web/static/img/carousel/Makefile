# Makefile to convert .png files to .webp

# Directory containing the images
IMG_DIR := .

# Find all .png files in the directory
PNG_FILES := $(wildcard $(IMG_DIR)/*.png)

# Convert .png files to .webp
WEBP_FILES := $(PNG_FILES:.png=.webp)

all: $(WEBP_FILES)

%.webp: %.png
	cwebp -q 80 $< -o $@

clean:
	rm -f $(WEBP_FILES)

.PHONY: all clean
