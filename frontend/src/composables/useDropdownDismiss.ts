import { isRef, onMounted, onUnmounted, type Ref } from 'vue'

export interface DropdownDismissOptions {
  /** 下拉是否展开：接受 Ref、普通布尔值或返回布尔值的函数 */
  isOpen: Ref<boolean> | boolean | (() => boolean)
  /** 收起下拉 */
  close: () => void
  /**
   * 下拉内部区域（触发器容器、弹层元素等）。
   * 事件目标落在这些元素内部时不收起，因此可在弹层被 Teleport 到 body 时同时传入两处元素。
   */
  inside: () => Array<Element | null | undefined>
}

/**
 * 下拉弹层在"失去焦点"时收起：
 * - 鼠标按下下拉以外的区域（捕获阶段监听，避免被弹层或对话框内部的 stopPropagation 拦住）
 * - 焦点移出下拉（Tab 切换到其他输入框、程序化聚焦）
 * - 浏览器窗口/标签页失去焦点
 */
export function useDropdownDismiss(options: DropdownDismissOptions): void {
  const isActive = (): boolean => {
    const open = options.isOpen
    if (typeof open === 'function') return open()
    if (isRef(open)) return open.value
    return open
  }

  const isInside = (target: EventTarget | null): boolean => {
    if (!(target instanceof Node)) return false
    return options.inside().some((element) => !!element && (element === target || element.contains(target)))
  }

  const handlePointerDown = (event: MouseEvent) => {
    if (!isActive() || isInside(event.target)) return
    options.close()
  }

  const handleFocusChange = (event: FocusEvent) => {
    if (!isActive() || isInside(event.target)) return
    options.close()
  }

  const handleWindowBlur = () => {
    if (!isActive()) return
    options.close()
  }

  onMounted(() => {
    document.addEventListener('mousedown', handlePointerDown, true)
    document.addEventListener('focusin', handleFocusChange, true)
    window.addEventListener('blur', handleWindowBlur)
  })

  onUnmounted(() => {
    document.removeEventListener('mousedown', handlePointerDown, true)
    document.removeEventListener('focusin', handleFocusChange, true)
    window.removeEventListener('blur', handleWindowBlur)
  })
}
