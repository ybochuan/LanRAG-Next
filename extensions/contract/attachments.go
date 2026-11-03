package contract

// RelatedAttachment points to a native, authenticated document download.
type RelatedAttachment struct {
	KnowledgeID      string `json:"knowledge_id"`
	KnowledgeBaseID  string `json:"knowledge_base_id"`
	ParentDocumentID string `json:"parent_document_id"`
	FileName         string `json:"file_name"`
	DownloadURL      string `json:"download_url"`
}

// AfterRetrieveResponse enriches only the supplied hit IDs; it grants no file capability.
type AfterRetrieveResponse struct {
	RequestID          string                         `json:"request_id"`
	TenantID           uint64                         `json:"tenant_id"`
	KnowledgeBaseIDs   []string                       `json:"knowledge_base_ids"`
	RelatedAttachments map[string][]RelatedAttachment `json:"related_attachments"`
}
