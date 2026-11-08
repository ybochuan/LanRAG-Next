package contract

// RelatedAttachment points to a native, authenticated document download.
// Semantic fields are optional; FileName and DownloadURL remain the stable
// native contract and must always be present.
type RelatedAttachment struct {
	KnowledgeID        string  `json:"knowledge_id"`
	KnowledgeBaseID    string  `json:"knowledge_base_id"`
	ParentDocumentID   string  `json:"parent_document_id"`
	FileName           string  `json:"file_name"`
	DownloadURL        string  `json:"download_url"`
	DisplayName        string  `json:"display_name,omitempty"`
	OriginalFilename   string  `json:"original_filename,omitempty"`
	AttachmentSequence int     `json:"attachment_sequence,omitempty"`
	ReferenceTitle     string  `json:"reference_title,omitempty"`
	ReferenceLocation  string  `json:"reference_location,omitempty"`
	ReferenceExcerpt   string  `json:"reference_excerpt,omitempty"`
	MappingStatus      string  `json:"mapping_status,omitempty"`
	MappingConfidence  float64 `json:"mapping_confidence,omitempty"`
	ParentTitle        string  `json:"parent_title,omitempty"`
	IndexPolicy        string  `json:"index_policy,omitempty"`
}

// AfterRetrieveResponse enriches only the supplied hit IDs; it grants no file capability.
type AfterRetrieveResponse struct {
	RequestID          string                         `json:"request_id"`
	TenantID           uint64                         `json:"tenant_id"`
	KnowledgeBaseIDs   []string                       `json:"knowledge_base_ids"`
	RelatedAttachments map[string][]RelatedAttachment `json:"related_attachments"`
}
