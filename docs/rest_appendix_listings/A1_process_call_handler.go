// Листинг А.1. REST-обработчик полного сценария process-call.
package handlers

import (
    "fmt"
    "log"
    "mime/multipart"
    "net/http"
    "os"
    "path/filepath"
    "strings"
    "time"

    "orchestrator/internal/services"

    "github.com/gin-gonic/gin"
)

// ProcessCall godoc
// @Summary Обработать аудио звонка
// @Description Загружает аудио файл, транскрибирует, определяет интент и создает тикет
// @Tags calls
// @Accept multipart/form-data
// @Produce json
// @Param audio formData file true "Audio file (mp3, wav, m4a)"
// @Success 200 {object} services.ProcessCallResult
// @Failure 400 {object} map[string]string
// @Failure 500 {object} map[string]string
// @Router /api/v1/process-call [post]
func (h *ProcessHandler) ProcessCall(c *gin.Context) {
    requestReceivedAt := time.Now().UTC()

    // Загрузка и проверка аудио
    file, originalName, ext, ok := h.loadAudioUpload(c)
    if !ok {
        return
    }

    // Файл сохраняется на диск
    audioPath, cleanup, ok := h.saveAudioUpload(c, file, ext)
    if !ok {
        return
    }
    if cleanup != nil {
        defer cleanup()
    }

    // Запуск основного конвейера обработки звонка
    result, ok := h.runProcessPipeline(c, audioPath, ext, file.Size)
    if !ok {
        return
    }

    h.finalizeProcessResult(result, requestReceivedAt, originalName)
    h.writeProcessSuccessAudit(c, result, ext, file.Size)
    c.JSON(http.StatusOK, result)
}

// Функция отвечает за получение и базовую проверку аудиофайла
func (h *ProcessHandler) loadAudioUpload(c *gin.Context) (*multipart.FileHeader, string, string, bool) {
    file, err := c.FormFile("audio")
    if err != nil {
        h.writeAudit(c, "call.process", "call", "", "failed", map[string]interface{}{
            "reason": "missing_audio",
        })
        c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is required"})
        return nil, "", "", false
    }

    originalName := filepath.Base(file.Filename)
    ext := strings.ToLower(filepath.Ext(originalName))

    // Проверка на поступившего файла на доступные расширения
    if !isAllowedAudioExt(ext) {
        h.writeAudit(c, "call.process", "call", "", "failed", map[string]interface{}{
            "reason":     "unsupported_audio_format",
            "audio_ext":  ext,
            "audio_size": file.Size,
        })
        c.JSON(http.StatusBadRequest, gin.H{
            "error": fmt.Sprintf("unsupported audio format: %s (allowed: mp3, wav, m4a, flac, ogg)", ext),
        })
        return nil, "", "", false
    }

    if file.Size == 0 {
        h.writeAudit(c, "call.process", "call", "", "failed", map[string]interface{}{
            "reason":     "empty_audio_file",
            "audio_ext":  ext,
            "audio_size": file.Size,
        })
        c.JSON(http.StatusBadRequest, gin.H{"error": "audio file is empty"})
        return nil, "", "", false
    }

    return file, originalName, ext, true
}

// Функция запускает основной конвейер обработки звонка
func (h *ProcessHandler) runProcessPipeline(
    c *gin.Context,
    audioPath string,
    ext string,
    audioSize int64,
) (*services.ProcessCallResult, bool) {

    // Вызов сервиса ProcessCall модуля управления
    result, err := h.orchestrator.ProcessCall(audioPath)
    if err != nil {
        log.Printf("Processing failed: %v", err)
        _ = os.Remove(audioPath)
        h.writeAudit(c, "call.process", "call", "", "failed", map[string]interface{}{
            "reason":     "pipeline_failed",
            "audio_ext":  ext,
            "audio_size": audioSize,
        })
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": fmt.Sprintf("processing failed: %v", err),
        })
        return nil, false
    }
    if result == nil {
        h.writeAudit(c, "call.process", "call", "", "failed", map[string]interface{}{
            "reason": "empty_pipeline_result",
        })
        c.JSON(http.StatusInternalServerError, gin.H{
            "error": "processing failed: empty pipeline result",
        })
        return nil, false
    }
    return result, true
}
