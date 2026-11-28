import { BUILTIN_QUICK_ANSWER_ID } from '@/api/agent'
import { useStartChat } from '@/components/GlobalCommandPalette/useStartChat'
import { useSettingsStore } from '@/stores/settings'
import type { ReaderQuestion } from './model'

export function useReaderQuickAnswer() {
  const settings = useSettingsStore()
  const { startChat } = useStartChat()

  function openQuestion(query: string, question: ReaderQuestion, origin = question) {
    const documents = question.right ? [question.left, question.right] : [question.left]
    settings.selectAgent(BUILTIN_QUICK_ANSWER_ID)
    settings.setFileKbMap(Object.fromEntries(documents.map(doc => [doc.id, doc.kb])))
    return startChat(query, [], documents.map(doc => doc.id), { readerQuestion: JSON.stringify(origin) })
  }

  return { openQuestion }
}
