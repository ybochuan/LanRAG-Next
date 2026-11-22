import { DialogPlugin } from 'tdesign-vue-next'

export interface ConfirmActionOptions {
  title?: string
  body: string
  confirmText?: string
  cancelText?: string
  danger?: boolean
}

/**
 * Promise wrapper around TDesign DialogPlugin.confirm. Native WeKnora pages use
 * DialogPlugin for blocking confirmations and t-popconfirm for inline actions;
 * window.confirm and hand-written <dialog> shells are not part of that system.
 */
export function confirmAction(options: ConfirmActionOptions): Promise<boolean> {
  return new Promise((resolve) => {
    let settled = false
    const finish = (value: boolean) => {
      if (settled) return
      settled = true
      resolve(value)
    }
    const dialog = DialogPlugin.confirm({
      header: options.title || '请确认',
      body: options.body,
      theme: options.danger ? 'danger' : 'default',
      confirmBtn: {
        content: options.confirmText || '确认',
        theme: options.danger ? 'danger' : 'primary',
      },
      cancelBtn: options.cancelText || '取消',
      onConfirm: () => { finish(true); dialog.hide() },
      onCancel: () => { finish(false); dialog.hide() },
      onClose: () => { finish(false); dialog.hide() },
    })
  })
}
