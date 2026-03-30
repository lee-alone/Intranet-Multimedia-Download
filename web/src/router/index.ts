import { createRouter, createWebHistory, type RouteMeta } from 'vue-router'
import type { RouteRecordRaw } from 'vue-router'
import type { Component } from 'vue'

// 扩展路由元信息类型
interface CustomRouteMeta extends RouteMeta {
  requiresAuth?: boolean
  requiresGuest?: boolean
  requiresMFA?: boolean
  title?: string
}

// 组件懒加载工厂函数（带类型）
// 使用静态导入以支持 Vite 构建
const viewComponents: Record<string, () => Promise<Component>> = {
  Login: () => import('@/views/Login.vue'),
  Register: () => import('@/views/Register.vue'),
  Dashboard: () => import('@/views/Dashboard.vue'),
  Tasks: () => import('@/views/Tasks.vue'),
  NewTask: () => import('@/views/NewTask.vue'),
  Audit: () => import('@/views/Audit.vue'),
  MfaBind: () => import('@/views/MfaBind.vue'),
  NotFound: () => import('@/views/NotFound.vue'),
}

function lazyLoad(viewName: string): () => Promise<Component> {
  return viewComponents[viewName] || viewComponents.NotFound
}

const routes: RouteRecordRaw[] = [
  {
    path: '/login',
    name: 'Login',
    component: lazyLoad('Login'),
    meta: {
      requiresGuest: true,
      title: '登录',
    } as CustomRouteMeta,
  },
  {
    path: '/register',
    name: 'Register',
    component: lazyLoad('Register'),
    meta: {
      requiresGuest: true,
      title: '注册',
    } as CustomRouteMeta,
  },
  {
    path: '/',
    name: 'Layout',
    component: () => import('@/layouts/MainLayout.vue'),
    meta: {
      requiresAuth: true,
      title: '首页',
    } as CustomRouteMeta,
    children: [
      {
        path: '',
        name: 'Dashboard',
        component: lazyLoad('Dashboard'),
        meta: {
          title: '仪表盘',
        } as CustomRouteMeta,
      },
      {
        path: 'tasks',
        name: 'Tasks',
        component: lazyLoad('Tasks'),
        meta: {
          title: '任务列表',
        } as CustomRouteMeta,
      },
      {
        path: 'tasks/new',
        name: 'NewTask',
        component: lazyLoad('NewTask'),
        meta: {
          title: '新建任务',
        } as CustomRouteMeta,
      },
     {
       path: 'audit',
       name: 'Audit',
       component: lazyLoad('Audit'),
       meta: {
         requiresMFA: true,
         title: '审计日志',
       } as CustomRouteMeta,
     },
     {
       path: 'mfa-bind',
       name: 'MfaBind',
       component: lazyLoad('MfaBind'),
       meta: {
         title: 'MFA 绑定',
       } as CustomRouteMeta,
     },
    ],
  },
  {
    path: '/:pathMatch(.*)*',
    name: 'NotFound',
    component: lazyLoad('NotFound'),
    meta: {
      title: '页面未找到',
    } as CustomRouteMeta,
  },
]

const router = createRouter({
  history: createWebHistory(),
  routes,
})

// 路由守卫 - 权限控制
router.beforeEach((to, _from, next) => {
  const token = localStorage.getItem('token')
  const isAuthenticated = !!token
  const meta = to.meta as CustomRouteMeta

  // 设置页面标题
  if (meta.title) {
    document.title = `${meta.title} - 校园资源采集系统`
  }

  // 需要认证的路由
  if (meta.requiresAuth && !isAuthenticated) {
    next({
      name: 'Login',
      query: { redirect: to.fullPath },
      replace: true,
    })
    return
  }

  // 已登录用户访问登录页，重定向到首页
  if (meta.requiresGuest && isAuthenticated) {
    next({ name: 'Dashboard', replace: true })
    return
  }

 // 检查 MFA 要求
 if (meta.requiresMFA) {
   const userMfaEnabled = localStorage.getItem('mfaEnabled')
   if (!userMfaEnabled) {
     // 跳转到 MFA 绑定页面
     next({ name: 'MfaBind', query: { redirect: to.fullPath } })
     return
   }
 }
 
 next()
})

// 路由后置钩子 - 页面滚动到顶部
router.afterEach(() => {
  window.scrollTo({ top: 0, behavior: 'smooth' })
})

export default router
