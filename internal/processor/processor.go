package processor

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/mirceanton/streamarr/internal/db"
	"github.com/mirceanton/streamarr/internal/models"
	"github.com/mirceanton/streamarr/internal/scanner"
)

var (
	processorOnce sync.Once
	jobCh         = make(chan int64, 100)
)

// Start begins the background job processor with the configured number of parallel workers.
func Start() {
	processorOnce.Do(func() {
		workers := db.GetParallelJobs()
		if workers < 1 {
			workers = 1
		}
		log.Printf("starting %d job worker(s)", workers)
		for i := 0; i < workers; i++ {
			go processLoop()
		}
	})
}

// Enqueue adds a job ID to the processing queue.
func Enqueue(jobID int64) {
	jobCh <- jobID
}

func processLoop() {
	for jobID := range jobCh {
		processJob(jobID)
	}
}

func processJob(jobID int64) {
	job, err := db.GetJob(jobID)
	if err != nil {
		log.Printf("get job %d: %v", jobID, err)
		return
	}

	if job.Status != "pending" {
		return
	}

	db.UpdateJobStatus(jobID, "running")

	var ops []models.Operation
	if err := json.Unmarshal([]byte(job.Operations), &ops); err != nil {
		failJob(jobID, fmt.Sprintf("parse operations: %v", err))
		return
	}

	mf, err := db.GetMediaFile(job.MediaFileID)
	if err != nil {
		failJob(jobID, fmt.Sprintf("get media file: %v", err))
		return
	}

	// Delete external subtitle files
	for _, op := range ops {
		if op.Type == "delete_external_subtitle" {
			if err := os.Remove(op.SourcePath); err != nil {
				failJob(jobID, fmt.Sprintf("delete external subtitle %s: %v", filepath.Base(op.SourcePath), err))
				return
			}
		}
	}

	// Execute embed operations first — new streams are appended, so original indices stay valid
	currentSubCount := len(mf.SubtitleTracks)
	for _, op := range ops {
		if op.Type == "embed_subtitle" {
			if err := embedSubtitle(mf.Path, op, currentSubCount); err != nil {
				failJob(jobID, fmt.Sprintf("embed subtitle %s: %v", filepath.Base(op.SourcePath), err))
				return
			}
			currentSubCount++
		}
	}

	// Execute extract operations
	for _, op := range ops {
		if op.Type == "extract_subtitle" {
			if err := extractSubtitle(mf.Path, op); err != nil {
				failJob(jobID, fmt.Sprintf("extract subtitle stream %d: %v", op.StreamIndex, err))
				return
			}
		}
	}

	// Collect streams to remove
	var removeIndices []int
	for _, op := range ops {
		if op.Type == "remove_audio" || op.Type == "remove_subtitle" {
			removeIndices = append(removeIndices, op.StreamIndex)
		}
	}

	if len(removeIndices) > 0 {
		cmd, err := buildRemoveCommand(mf.Path, removeIndices)
		if err != nil {
			failJob(jobID, fmt.Sprintf("build ffmpeg command: %v", err))
			return
		}

		db.UpdateJobCommand(jobID, strings.Join(cmd.Args, " "))

		output, err := cmd.CombinedOutput()
		if err != nil {
			failJob(jobID, fmt.Sprintf("ffmpeg failed: %v\nOutput: %s", err, string(output)))
			// Clean up temp file
			tmpPath := mf.Path + ".tmp" + filepath.Ext(mf.Path)
			os.Remove(tmpPath)
			return
		}

		// Atomic rename
		tmpPath := mf.Path + ".tmp" + filepath.Ext(mf.Path)
		if err := os.Rename(tmpPath, mf.Path); err != nil {
			failJob(jobID, fmt.Sprintf("rename temp file: %v", err))
			return
		}
	}

	// Re-probe and update DB
	audioTracks, subtitleTracks, err := scanner.Probe(mf.Path)
	if err != nil {
		log.Printf("re-probe after job %d: %v", jobID, err)
	} else {
		db.DeleteTracksForFile(mf.ID)
		preferredLangs, _ := db.GetPreferredLanguages()
		// Use per-item override if one is set, matching the scanner's behavior
		effectiveLangs := preferredLangs
		if len(mf.LanguageOverride) > 0 {
			effectiveLangs = mf.LanguageOverride
		}

		for _, t := range audioTracks {
			t.MediaFileID = mf.ID
			db.InsertAudioTrack(&t)
		}

		for _, t := range subtitleTracks {
			t.MediaFileID = mf.ID
			db.InsertSubtitleTrack(&t)
		}

		needsAttention, attentionReasons := scanner.ComputeAttentionReasons(audioTracks, subtitleTracks, effectiveLangs)

		// Update file info
		info, _ := os.Stat(mf.Path)
		if info != nil {
			db.DB.Exec(`UPDATE media_files SET size_bytes = ?, scanned_at = ?, needs_attention = ?, attention_reasons = ? WHERE id = ?`,
				info.Size(), time.Now(), needsAttention, attentionReasons, mf.ID)
		}
	}

	// Refresh external subtitle file list (picks up any newly extracted sidecar files)
	if err := scanner.ScanExternalSubtitles(mf); err != nil {
		log.Printf("scan external subtitles after job %d: %v", jobID, err)
	}

	db.UpdateJobStatus(jobID, "done")
}

func extractSubtitle(inputPath string, op models.Operation) error {
	outputPath := op.OutputPath
	if outputPath == "" {
		return fmt.Errorf("no output path specified for extraction")
	}

	cmd := exec.Command("ffmpeg",
		"-y",
		"-i", inputPath,
		"-map", fmt.Sprintf("0:%d", op.StreamIndex),
		"-c", "copy",
		outputPath,
	)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v: %s", err, string(output))
	}
	return nil
}

func embedSubtitle(inputPath string, op models.Operation, newSubStreamIdx int) error {
	if op.SourcePath == "" {
		return fmt.Errorf("no source path specified for embed")
	}

	args := []string{
		"-y",
		"-i", inputPath,
		"-i", op.SourcePath,
		"-map", "0",
		"-map", "1:0",
		"-c", "copy",
	}

	if op.Language != "" {
		args = append(args,
			fmt.Sprintf("-metadata:s:s:%d", newSubStreamIdx),
			fmt.Sprintf("language=%s", op.Language),
		)
	}

	if op.Forced || op.SDH {
		var dispositions []string
		if op.Forced {
			dispositions = append(dispositions, "forced")
		}
		if op.SDH {
			dispositions = append(dispositions, "hearing_impaired")
		}
		args = append(args,
			fmt.Sprintf("-disposition:s:s:%d", newSubStreamIdx),
			strings.Join(dispositions, "+"),
		)
	}

	tmpPath := inputPath + ".tmp" + filepath.Ext(inputPath)
	args = append(args, tmpPath)

	cmd := exec.Command("ffmpeg", args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("%v: %s", err, string(output))
	}

	if err := os.Rename(tmpPath, inputPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename temp file: %v", err)
	}

	// Delete the source file after it has been successfully embedded
	if err := os.Remove(op.SourcePath); err != nil {
		log.Printf("warning: remove embedded subtitle source %s: %v", op.SourcePath, err)
	}

	return nil
}

func buildRemoveCommand(inputPath string, removeIndices []int) (*exec.Cmd, error) {
	// Get all stream indices via ffprobe
	audioTracks, subtitleTracks, err := scanner.Probe(inputPath)
	if err != nil {
		return nil, err
	}

	removeSet := make(map[int]bool)
	for _, idx := range removeIndices {
		removeSet[idx] = true
	}

	// Build -map arguments: include all streams EXCEPT the ones being removed
	// We need to discover all stream indices. We'll use -map 0 and then -map -0:idx for removals.
	// Actually, it's cleaner to explicitly map what we want.
	// But we don't know video stream indices from our data. Let's use the negative mapping approach.

	args := []string{
		"-y",
		"-i", inputPath,
		"-map", "0",
	}

	// Add negative mappings for streams to remove
	for _, idx := range removeIndices {
		args = append(args, "-map", fmt.Sprintf("-0:%d", idx))
	}

	args = append(args, "-c", "copy")

	tmpPath := inputPath + ".tmp" + filepath.Ext(inputPath)
	args = append(args, tmpPath)

	// Validate: ensure we're not removing all audio tracks
	remainingAudio := 0
	for _, t := range audioTracks {
		if !removeSet[t.StreamIndex] {
			remainingAudio++
		}
	}
	if remainingAudio == 0 && len(audioTracks) > 0 {
		return nil, fmt.Errorf("cannot remove all audio tracks")
	}

	_ = subtitleTracks // subtitles can all be removed

	cmd := exec.Command("ffmpeg", args...)
	return cmd, nil
}

func failJob(jobID int64, errMsg string) {
	log.Printf("job %d failed: %s", jobID, errMsg)
	db.UpdateJobError(jobID, errMsg)
	db.UpdateJobStatus(jobID, "failed")
}
