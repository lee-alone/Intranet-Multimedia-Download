/**
 * 任务轮询 Composable
 * 提供智能轮询控制，仅在有活跃任务时才轮询
 */

import { ref, type Ref } from 'vue'

// 任务状态类型
export type TaskStatus = 'queued' | 'downloading' | 'merging' | 'completed' | 'failed' | 'cancelled'

// 任务接口
export interface Task {
  id: string
  status: TaskStatus
  [key: string]: any
}

// 轮询配置
export interface PollingConfig {
  interval: number // 轮询间隔（毫秒）
  enabled: boolean // 是否启用
}

// 内部状态
let pollTimer: ReturnType<typeof setInterval> | null = null
const isPolling = ref(false)

/**
 * 启动轮询
 * @param callback 轮询回调函数
 * @param interval 轮询间隔（毫秒），默认 3000ms
 */
export function startPolling(callback: () => void | Promise<void>, interval: number = 3000) {
  if (pollTimer) {
    clearInterval(pollTimer)
  }

  pollTimer = setInterval(() => {
    callback()
  }, interval)

  isPolling.value = true
}

/**
 * 停止轮询
 */
export function stopPolling() {
  if (pollTimer) {
    clearInterval(pollTimer)
    pollTimer = null
  }
  isPolling.value = false
}

/**
 * 检查是否有活跃任务（需要继续轮询）
 * @param tasks 任务列表
 * @returns 是否有活跃任务
 */
export function hasActiveTasks(tasks: Ref<Task[]>): boolean {
  return tasks.value.some(t =>
    ['queued', 'downloading', 'merging'].includes(t.status)
  )
}

/**
 * 智能轮询控制：根据任务状态自动启停
 * @param tasks 任务列表
 * @param fetchCallback 获取数据的回调函数
 */
export function updatePolling(
  tasks: Ref<Task[]>,
  fetchCallback: () => void | Promise<void>
) {
  const active = hasActiveTasks(tasks)

  if (active && !pollTimer) {
    // 有活跃任务且未轮询，启动轮询
    console.log('检测到活跃任务，启动轮询')
    startPolling(fetchCallback)
  } else if (!active && pollTimer) {
    // 无活跃任务且在轮询，停止轮询（节约资源）
    console.log('无活跃任务，停止轮询')
    stopPolling()
  }
}

/**
 * 获取轮询状态
 */
export function getPollingStatus(): boolean {
  return isPolling.value
}

/**
 * 轮询 Composable 主函数
 * @param fetchCallback 获取数据的回调函数
 * @param config 轮询配置
 */
export function useTaskPolling(
  fetchCallback: () => void | Promise<void>,
  config: PollingConfig = { interval: 3000, enabled: true }
) {
  const pollingEnabled = ref(config.enabled)

  /**
   * 启动智能轮询
   */
  function startSmartPolling() {
    if (!pollingEnabled.value) return
    startPolling(fetchCallback, config.interval)
  }

  /**
   * 停止轮询
   */
  function stopSmartPolling() {
    stopPolling()
  }

  /**
   * 根据任务状态更新轮询
   */
  function updateSmartPolling(tasks: Task[]) {
    const active = tasks.some(t =>
      ['queued', 'downloading', 'merging'].includes(t.status)
    )

    if (active && !isPolling.value) {
      console.log('检测到活跃任务，启动轮询')
      startSmartPolling()
    } else if (!active && isPolling.value) {
      console.log('无活跃任务，停止轮询')
      stopSmartPolling()
    }
  }

  return {
    isPolling,
    startSmartPolling,
    stopSmartPolling,
    updateSmartPolling,
    hasActiveTasks: (tasks: Ref<Task[]>) => hasActiveTasks(tasks),
  }
}
