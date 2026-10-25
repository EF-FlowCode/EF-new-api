import assert from 'node:assert/strict'
import { describe, it } from 'node:test'
import {
  getOutOfRangePagePatch,
  getServerPageResetPatch,
  getServerPaginationMeta,
  getServerPaginationSearchPatch,
  readServerPagination,
} from './server-pagination-state'

describe('server pagination URL state', () => {
  it('reads page and page size from custom URL keys', () => {
    assert.deepEqual(
      readServerPagination(
        { subjectPage: 3, subjectPageSize: 50 },
        {
          pageKey: 'subjectPage',
          pageSizeKey: 'subjectPageSize',
          defaultPageSize: 10,
        }
      ),
      { pageIndex: 2, pageSize: 50 }
    )
  })

  it('falls back to valid defaults for invalid URL values', () => {
    assert.deepEqual(readServerPagination({ page: -2, pageSize: 0 }), {
      pageIndex: 0,
      pageSize: 20,
    })
  })

  it('writes non-default values and removes defaults from the URL', () => {
    assert.deepEqual(
      getServerPaginationSearchPatch(
        { pageIndex: 2, pageSize: 50 },
        { defaultPageSize: 10 }
      ),
      { page: 3, pageSize: 50 }
    )

    assert.deepEqual(
      getServerPaginationSearchPatch(
        { pageIndex: 0, pageSize: 10 },
        { defaultPageSize: 10 }
      ),
      { page: undefined, pageSize: undefined }
    )
  })

  it('resets the configured page key when a query changes', () => {
    assert.deepEqual(getServerPageResetPatch({ pageKey: 'incidentPage' }), {
      incidentPage: undefined,
    })
  })

  it('corrects a page that exceeds the server total', () => {
    assert.deepEqual(getOutOfRangePagePatch({ page: 8 }, 3), {
      page: undefined,
    })
    assert.deepEqual(getOutOfRangePagePatch({ page: 8 }, 3, {}, 'last'), {
      page: 3,
    })
    assert.equal(getOutOfRangePagePatch({ page: 2 }, 3), null)
  })
})

describe('server pagination availability', () => {
  it('derives page count and previous/next availability from total', () => {
    assert.deepEqual(
      getServerPaginationMeta(45, { pageIndex: 0, pageSize: 20 }),
      {
        pageCount: 3,
        canPreviousPage: false,
        canNextPage: true,
      }
    )
    assert.deepEqual(
      getServerPaginationMeta(45, { pageIndex: 2, pageSize: 20 }),
      {
        pageCount: 3,
        canPreviousPage: true,
        canNextPage: false,
      }
    )
  })

  it('disables both directions when the server returns no rows', () => {
    assert.deepEqual(
      getServerPaginationMeta(0, { pageIndex: 0, pageSize: 20 }),
      {
        pageCount: 0,
        canPreviousPage: false,
        canNextPage: false,
      }
    )
  })
})
