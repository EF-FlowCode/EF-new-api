import { useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { getRouteApi } from '@tanstack/react-router'
import {
  type SortingState,
  type VisibilityState,
  flexRender,
  getCoreRowModel,
  getFacetedRowModel,
  getFacetedUniqueValues,
  getFilteredRowModel,
  getPaginationRowModel,
  useReactTable,
} from '@tanstack/react-table'
import { useMediaQuery } from '@/hooks'
import { Search } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { cn } from '@/lib/utils'
import { useTableUrlState } from '@/hooks/use-table-url-state'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  DISABLED_ROW_DESKTOP,
  DISABLED_ROW_MOBILE,
  DataTablePagination,
  DataTableToolbar,
  TableSkeleton,
  TableEmpty,
  MobileCardList,
} from '@/components/data-table'
import { PageFooterPortal } from '@/components/layout'
import { getUserDistinctGroups, getUsers, searchUsers } from '../api'
import {
  USER_STATUS,
  getUserStatusOptions,
  getUserRoleOptions,
  isUserDeleted,
} from '../constants'
import type { User } from '../types'
import { DataTableBulkActions } from './data-table-bulk-actions'
import { useUsersColumns } from './users-columns'
import { useUsers } from './users-provider'

const route = getRouteApi('/_authenticated/users/')

function isDisabledUserRow(user: User) {
  return isUserDeleted(user) || user.status === USER_STATUS.DISABLED
}

export function UsersTable() {
  const { t } = useTranslation()
  const columns = useUsersColumns()
  const { refreshTrigger } = useUsers()
  const isMobile = useMediaQuery('(max-width: 640px)')
  const [rowSelection, setRowSelection] = useState({})
  const [sorting, setSorting] = useState<SortingState>([])
  const [columnVisibility, setColumnVisibility] = useState<VisibilityState>({
    invite_info: false,
    created_at: false,
    last_login_at: false,
  })

  const search = route.useSearch()
  const navigate = route.useNavigate()

  const {
    columnFilters,
    onColumnFiltersChange,
    pagination,
    onPaginationChange,
    ensurePageInRange,
  } = useTableUrlState({
    search,
    navigate,
    pagination: { defaultPage: 1, defaultPageSize: isMobile ? 10 : 20 },
    // eflow: 旧的单一关键字搜索已被下面三个字段级搜索框取代，禁用全局 globalFilter
    globalFilter: { enabled: false },
    columnFilters: [
      // eflow: 三个 columnFilter 都是 type:'array' (DataTableFacetedFilter
      // 内部统一用 string[] 表达 selectedValues，单选 facet 也是 string[length=1])。
      // 后端：status / role 走 IN (?)；group 是单值——前端取 array[0] 透传。
      { columnId: 'status', searchKey: 'status', type: 'array' },
      { columnId: 'role', searchKey: 'role', type: 'array' },
      { columnId: 'group', searchKey: 'group', type: 'array' },
    ],
  })

  // eflow: 三个独立搜索框（ID / 用户名 / 邮箱），各自一个 URL query，互不串字段。
  // URL 为已提交的数据源；本地 state 承载输入框草稿，点击 Search 或按 Enter 后提交。
  const searchId = search.search_id ?? ''
  const searchName = search.search_name ?? ''
  const searchEmail = search.search_email ?? ''
  const [idInput, setIdInput] = useState(searchId)
  const [nameInput, setNameInput] = useState(searchName)
  const [emailInput, setEmailInput] = useState(searchEmail)
  // URL → 本地（处理重置 / 浏览器前进后退等外部变更）
  useEffect(() => setIdInput(searchId), [searchId])
  useEffect(() => setNameInput(searchName), [searchName])
  useEffect(() => setEmailInput(searchEmail), [searchEmail])
  const applyScopedSearch = () => {
    const nid = idInput.trim()
    const nname = nameInput.trim()
    const nemail = emailInput.trim()
    if (nid === searchId && nname === searchName && nemail === searchEmail)
      return
    navigate({
      search: (prev) => ({
        ...prev,
        page: undefined,
        search_id: nid || undefined,
        search_name: nname || undefined,
        search_email: nemail || undefined,
      }),
    })
  }
  const hasScopedSearch = !!(searchId || searchName || searchEmail)
  const clearScopedSearch = () => {
    setIdInput('')
    setNameInput('')
    setEmailInput('')
    navigate({
      search: (prev) => ({
        ...prev,
        page: undefined,
        search_id: undefined,
        search_name: undefined,
        search_email: undefined,
      }),
    })
  }

  // 后端 model.NewUserSortOptions 白名单：id/username/quota/status/role/created_at
  const sortBy = sorting[0]?.id
  const sortOrder: 'asc' | 'desc' | undefined = sorting[0]
    ? sorting[0].desc
      ? 'desc'
      : 'asc'
    : undefined

  // eflow: role / status / group 三个筛选都接到后端，跨页正确。
  // - role / status: columnFilter 多选 (DataTableFacetedFilter)，value 是 string[]
  //   → 转 number[] 喂后端按 IN (?) 过滤；空值不挂 query。
  // - group: 单选 facet（singleSelect=true），value 仍是 string[]（长度 0 或 1），
  //   取 [0] 喂后端按 = 过滤。
  const roleFilterRaw = columnFilters.find((f) => f.id === 'role')?.value as
    | string[]
    | undefined
  const roleFilter =
    roleFilterRaw && roleFilterRaw.length > 0
      ? roleFilterRaw.map((v) => Number(v)).filter((n) => !Number.isNaN(n))
      : undefined

  const statusFilterRaw = columnFilters.find((f) => f.id === 'status')
    ?.value as string[] | undefined
  const statusFilter =
    statusFilterRaw && statusFilterRaw.length > 0
      ? statusFilterRaw.map((v) => Number(v)).filter((n) => !Number.isNaN(n))
      : undefined

  const groupFilterRaw = columnFilters.find((f) => f.id === 'group')?.value as
    | string[]
    | undefined
  const groupFilter =
    groupFilterRaw && groupFilterRaw.length > 0 ? groupFilterRaw[0] : undefined

  // eflow: 拉一次 users.group 列实际出现过的分组名给 group facet 选项。
  // 走 /api/user/distinct_groups（staffReadRoute），admin/客服都能调。
  // 不走 /api/group/——那是「倍率配置里的分组」，跟 users.group 字段不一致
  // (worker 这种历史分组在倍率配置里没有)，会让筛选下拉看不到真实分布。
  const { data: groupOptions } = useQuery({
    queryKey: ['users-table-distinct-groups'],
    queryFn: async () => {
      const res = await getUserDistinctGroups()
      if (!res.success || !Array.isArray(res.data)) return []
      return res.data.sort((a, b) => a.localeCompare(b))
    },
    staleTime: 5 * 60 * 1000,
  })

  // Fetch data with React Query
  const { data, isLoading, isFetching } = useQuery({
    queryKey: [
      'users',
      pagination.pageIndex + 1,
      pagination.pageSize,
      searchId,
      searchName,
      searchEmail,
      hasScopedSearch,
      sortBy,
      sortOrder,
      roleFilter?.join(','),
      statusFilter?.join(','),
      groupFilter,
      refreshTrigger,
    ],
    queryFn: async () => {
      const params = {
        p: pagination.pageIndex + 1,
        page_size: pagination.pageSize,
        sort_by: sortBy,
        sort_order: sortOrder,
        role: roleFilter,
        status: statusFilter,
        group: groupFilter,
      }

      // eflow: ID / 用户名 / 邮箱 任一非空 → searchUsers（后端字段级 AND）；否则普通列表
      const result = hasScopedSearch
        ? await searchUsers({
            ...params,
            search_id: searchId,
            search_username: searchName,
            search_email: searchEmail,
          })
        : await getUsers(params)

      if (!result.success) {
        toast.error(
          result.message ||
            `Failed to ${hasScopedSearch ? 'search' : 'load'} users`
        )
        return { items: [], total: 0 }
      }

      return {
        items: result.data?.items || [],
        total: result.data?.total || 0,
      }
    },
    placeholderData: (previousData) => previousData,
  })

  const users = data?.items || []

  const table = useReactTable({
    data: users,
    columns,
    state: {
      sorting,
      columnVisibility,
      rowSelection,
      columnFilters,
      pagination,
    },
    enableRowSelection: true,
    onRowSelectionChange: setRowSelection,
    onSortingChange: setSorting,
    onColumnVisibilityChange: setColumnVisibility,
    getCoreRowModel: getCoreRowModel(),
    getFilteredRowModel: getFilteredRowModel(),
    getPaginationRowModel: getPaginationRowModel(),
    getFacetedRowModel: getFacetedRowModel(),
    getFacetedUniqueValues: getFacetedUniqueValues(),
    onPaginationChange,
    onColumnFiltersChange,
    // eflow: 列表(getUsers)与搜索(searchUsers)两条路都是服务端分页+过滤——都传
    // p/page_size、role/status/group、keyword 到后端，并返回 total。之前
    // manualPagination 写成 !globalFilter，一旦进搜索态就退回客户端分页，只对
    // 服务器返回的当前页(20 条)做切片 → getPageCount 恒为 1，"下一页"被禁用，
    // 搜索结果跨页时翻不到第二页。统一设为 true；manualFiltering 同理设 true，
    // 避免客户端再用 globalFilterFn 对已被后端过滤过的当前页二次过滤而漏行。
    manualPagination: true,
    manualFiltering: true,
    manualSorting: true,
    pageCount: Math.ceil((data?.total || 0) / pagination.pageSize),
  })

  const pageCount = table.getPageCount()
  useEffect(() => {
    ensurePageInRange(pageCount)
  }, [pageCount, ensurePageInRange])

  // eflow: 三个独立搜索框替换原单一关键字框。ID 限数字输入；用户名框同时匹配
  // username 与 display_name（姓名）；邮箱框模糊匹配。三者互不串字段。
  const scopedSearch = (
    <div className='flex w-full flex-col gap-1.5 sm:w-auto sm:flex-row sm:items-center sm:gap-2'>
      <Input
        placeholder={t('User ID')}
        value={idInput}
        inputMode='numeric'
        onChange={(e) => setIdInput(e.target.value.replace(/[^0-9]/g, ''))}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            applyScopedSearch()
          }
        }}
        className='h-9 w-full sm:h-8 sm:w-[110px]'
      />
      <Input
        placeholder={t('Username')}
        value={nameInput}
        onChange={(e) => setNameInput(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            applyScopedSearch()
          }
        }}
        className='h-9 w-full sm:h-8 sm:w-[150px]'
      />
      <Input
        placeholder={t('Email')}
        value={emailInput}
        onChange={(e) => setEmailInput(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter') {
            e.preventDefault()
            applyScopedSearch()
          }
        }}
        className='h-9 w-full sm:h-8 sm:w-[180px]'
      />
      <Button
        variant='outline'
        size='sm'
        className='h-9 shrink-0 sm:h-8'
        onClick={applyScopedSearch}
      >
        <Search className='size-3.5' />
        {t('Search')}
      </Button>
    </div>
  )

  return (
    <>
      <div className='space-y-3 sm:space-y-4'>
        <DataTableToolbar
          table={table}
          customSearch={scopedSearch}
          hasAdditionalFilters={hasScopedSearch}
          onReset={clearScopedSearch}
          filters={[
            {
              columnId: 'status',
              title: t('Status'),
              options: getUserStatusOptions(t),
            },
            {
              columnId: 'role',
              title: t('Role'),
              options: getUserRoleOptions(t),
            },
            // eflow: 客户等级单选 facet，只筛选 users.group，不筛选授权令牌分组。
            // 没拉到时 fallback 空数组——facet 会展示 "No results found." 而非崩。
            {
              columnId: 'group',
              title: t('Customer tier'),
              options: (groupOptions ?? []).map((g) => ({
                label: g,
                value: g,
              })),
              singleSelect: true,
            },
          ]}
        />
        {isMobile ? (
          <MobileCardList
            table={table}
            isLoading={isLoading}
            emptyTitle={t('No Users Found')}
            emptyDescription={t(
              'No users available. Try adjusting your search or filters.'
            )}
            getRowClassName={(row) =>
              isDisabledUserRow(row.original) ? DISABLED_ROW_MOBILE : undefined
            }
          />
        ) : (
          <>
            <div
              className={cn(
                'overflow-x-auto rounded-md border transition-opacity duration-150',
                isFetching && !isLoading && 'pointer-events-none opacity-50'
              )}
            >
              <Table>
                <TableHeader>
                  {table.getHeaderGroups().map((headerGroup) => (
                    <TableRow key={headerGroup.id}>
                      {headerGroup.headers.map((header) => (
                        <TableHead key={header.id} colSpan={header.colSpan}>
                          {header.isPlaceholder
                            ? null
                            : flexRender(
                                header.column.columnDef.header,
                                header.getContext()
                              )}
                        </TableHead>
                      ))}
                    </TableRow>
                  ))}
                </TableHeader>
                <TableBody>
                  {isLoading ? (
                    <TableSkeleton table={table} keyPrefix='users-skeleton' />
                  ) : table.getRowModel().rows.length === 0 ? (
                    <TableEmpty
                      colSpan={columns.length}
                      title={t('No Users Found')}
                      description={t(
                        'No users available. Try adjusting your search or filters.'
                      )}
                    />
                  ) : (
                    table.getRowModel().rows.map((row) => {
                      const user = row.original

                      return (
                        <TableRow
                          key={row.id}
                          data-state={row.getIsSelected() && 'selected'}
                          className={cn(
                            isDisabledUserRow(user) && DISABLED_ROW_DESKTOP
                          )}
                        >
                          {row.getVisibleCells().map((cell) => (
                            <TableCell key={cell.id}>
                              {flexRender(
                                cell.column.columnDef.cell,
                                cell.getContext()
                              )}
                            </TableCell>
                          ))}
                        </TableRow>
                      )
                    })
                  )}
                </TableBody>
              </Table>
            </div>
            <DataTableBulkActions table={table} />
          </>
        )}
      </div>
      <PageFooterPortal>
        <DataTablePagination table={table} />
      </PageFooterPortal>
    </>
  )
}
