import { computed, ref, watch, type Ref } from 'vue'
import type { ListKnowledgeFilesParams } from '@/api/knowledge-base'
import { readerID } from './model'
import type { NativeReaderDocument } from './readerApi'
import { httpStatus } from '../business/model'

interface PickerPage {
  success?: boolean
  data?: NativeReaderDocument[] | null
  total: number
  page: number
  page_size: number
}

export function useReaderPickerFiles(
  kb: Ref<string>,
  keyword: Ref<string>,
  scope: () => string,
  listFiles: (kb: string, params: ListKnowledgeFilesParams) => Promise<unknown>,
) {
  const selection = ref('')
  const fileOptions = ref<{ label: string; value: string }[]>([])
  const filesLoading = ref(false)
  const filesError = ref(false)
  const nextPage = ref<number>()
  const normalizedKeyword = computed(() => keyword.value.trim())
  let requestPage: (() => Promise<void>) | undefined

  watch([kb, normalizedKeyword, scope], ([activeKB, search, activeScope], previous, onCleanup) => {
    let current = true
    let pending = false
    let pageSize = 50
    let timer: ReturnType<typeof setTimeout> | undefined
    onCleanup(() => { current = false; clearTimeout(timer) })
    requestPage = undefined
    selection.value = ''
    fileOptions.value = []
    nextPage.value = undefined
    filesLoading.value = !!activeKB
    filesError.value = false
    if (!activeKB) return

    nextPage.value = 1
    requestPage = async () => {
      const page = nextPage.value
      if (!current || pending || page === undefined) return
      pending = true
      filesLoading.value = true
      filesError.value = false
      try {
        const response = await listFiles(encodeURIComponent(activeKB), { page, page_size: pageSize, keyword: search }) as PickerPage
        if (!current) return
        if (response?.success !== true || response.page !== page || !Number.isSafeInteger(response.total) || response.total < 0 ||
          !Number.isSafeInteger(response.page_size) || response.page_size < 1 ||
          (!Array.isArray(response.data) && !(response.data === null && response.total === 0))) throw new Error('invalid_reader_picker_page')

        const options = new Map(fileOptions.value.map(option => [option.value, option]))
        for (const doc of response.data ?? []) {
          const id = readerID(doc?.id)
          if (id && doc.knowledge_base_id === activeKB && (doc.file_name || doc.type === 'file' || doc.type === 'manual')) {
            options.set(id, { label: doc.title || doc.file_name || id, value: id })
          }
        }
        fileOptions.value = [...options.values()]
        pageSize = response.page_size
        nextPage.value = page * pageSize < response.total ? page + 1 : undefined
      } catch (error) {
        if (!current) return
        const status = httpStatus(error)
        if (status === 401 || status === 403 || status === 404) {
          fileOptions.value = []
          selection.value = ''
          nextPage.value = 1
        }
        filesError.value = true
      } finally {
        pending = false
        if (current) filesLoading.value = false
      }
    }

    if (previous?.[0] === activeKB && previous[2] === activeScope) timer = setTimeout(requestPage, 250)
    else void requestPage()
  }, { immediate: true, flush: 'sync' })

  return {
    selection, fileOptions, filesLoading, filesError,
    hasMoreFiles: computed(() => (nextPage.value ?? 0) > 1),
    filesEmpty: computed(() => !!kb.value && !filesLoading.value && !filesError.value && !fileOptions.value.length && nextPage.value === undefined),
    loadMoreFiles: () => requestPage?.(),
  }
}
