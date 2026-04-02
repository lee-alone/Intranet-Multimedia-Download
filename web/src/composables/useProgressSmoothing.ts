/**
 * 进度平滑 Composable
 * 用于避免任务进度跳动，提供加权平滑算法
 */

// 平滑进度数据
export interface SmoothedProgress {
  smoothed: number // 加权平滑后的进度
  history: number[] // 最近几次的进度历史
  weights: number[] // 权重
  lastUpdated?: number // 最后更新时间戳
}

// 缓存 Map
const progressCache = new Map<string, SmoothedProgress>()

// 默认权重（越近的数据权重越高）
const DEFAULT_WEIGHTS = [0.4, 0.3, 0.2, 0.1]

/**
 * 加权平滑算法 - 避免进度跳动
 * @param taskId 任务 ID
 * @param newProgress 新的进度值
 * @returns 平滑后的进度
 */
export function smoothProgress(taskId: string, newProgress: number): number {
  const defaultWeights = DEFAULT_WEIGHTS

  if (!progressCache.has(taskId)) {
    progressCache.set(taskId, {
      smoothed: newProgress,
      history: [newProgress],
      weights: defaultWeights,
      lastUpdated: Date.now()
    })
    return newProgress
  }

  const data = progressCache.get(taskId)!
  const historySize = data.weights.length

  // 更新历史
  data.history.push(newProgress)
  if (data.history.length > historySize) {
    data.history.shift()
  }
  data.lastUpdated = Date.now()

  // 计算加权平均
  let weightedSum = 0
  let weightTotal = 0

  for (let i = 0; i < data.history.length; i++) {
    const weightIndex = Math.max(0, data.weights.length - (data.history.length - i))
    const weight = data.weights[weightIndex] || 0.1
    weightedSum += data.history[i] * weight
    weightTotal += weight
  }

  data.smoothed = weightedSum / weightTotal
  progressCache.set(taskId, data)

  return Math.round(data.smoothed * 100) / 100 // 保留两位小数
}

/**
 * 获取平滑后的进度
 * @param taskId 任务 ID
 * @param progress 当前进度
 * @returns 平滑后的进度
 */
export function getSmoothedProgress(taskId: string, progress: number): number {
  return smoothProgress(taskId, progress)
}

/**
 * 清理已完成任务的缓存
 * @param taskIds 已完成的任务 ID 列表
 */
export function cleanupFinishedTasks(taskIds: string[]) {
  taskIds.forEach(id => {
    progressCache.delete(id)
  })
}

/**
 * 清理超过指定时间未更新的缓存数据
 * @param maxAge 最大年龄（毫秒），默认 5 分钟
 */
export function cleanupStaleCache(maxAge: number = 5 * 60 * 1000) {
  const now = Date.now()
  for (const [taskId, data] of progressCache.entries()) {
    if (data.lastUpdated && now - data.lastUpdated > maxAge) {
      progressCache.delete(taskId)
    }
  }
}

/**
 * 清除所有缓存
 */
export function clearProgressCache() {
  progressCache.clear()
}

/**
 * 获取缓存大小
 */
export function getCacheSize(): number {
  return progressCache.size
}
