package guidance

import (
	"context"
	"database/sql"
	"fmt"
	"slices"

	"github.com/ybochuan/LanRAG-Next/extensions/corrections"
)

func correctionScope(s Scope) corrections.Scope {
	return corrections.Scope{TenantID: s.TenantID, KnowledgeBaseID: s.KnowledgeBaseID}
}

const correctionPrefix = "【已审核勘误】\n"

func addCorrections(ctx context.Context, tx *sql.Tx, s Scope, out *Selection, previous []corrections.Identity) error {
	if !s.CorrectionEvidence {
		return nil
	}
	scope := correctionScope(s)
	matches, err := corrections.EvidenceTx(ctx, tx, scope, s.Now, s.RunID, s.StepID, s.ConsumedToolSteps, s.Query)
	if err != nil {
		return err
	}
	for _, e := range matches {
		if previous != nil && !slices.Contains(previous, e.Identity) {
			continue
		}
		body := out.CorrectionContent
		if body == "" {
			body = correctionPrefix
		}
		line := fmt.Sprintf("- 勘误 %d\n  适用条件：%s\n  修正口径：%s\n", len(out.CorrectionItems)+1, e.AppliesWhen, e.Correction)
		if len(body)+len(line) > 6000 {
			continue
		}
		out.CorrectionContent = body + line
		out.CorrectionItems = append(out.CorrectionItems, e.Identity)
		out.AnnotationPolicy = annotationPolicy + "\n对【已审核勘误】，问题指向同一条款或其别名且满足适用条件时，直接以修正口径作答。原文相冲突的旧说法只可用于说明勘误缘由，不得当作结论或备选。使用提供的对应引用标识注明原文和勘误两项依据；其余条款按原文回答。"
		if len(out.CorrectionItems) == 6 {
			break
		}
	}
	return nil
}
