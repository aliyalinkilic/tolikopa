import { CatalogList } from '@/components/removal/CatalogList'
import type { RemediationPostureResponse } from '@/lib/types'
import { useT } from '@/i18n'
import { ItemProgressTable } from './ItemProgressTable'
import type { PolicyEditor } from './usePolicyEditor'

interface Props {
  editor: PolicyEditor
  posture: RemediationPostureResponse | undefined
  postureLoading: boolean
}

/** Bloatware removal: Appx packages from the compiled catalog. */
export function AppsTab({ editor, posture, postureLoading }: Props) {
  const t = useT()
  const { draft } = editor
  return (
    <div className="space-y-4">
      <CatalogList
        kind="app"
        catalog={editor.catalog}
        isSelected={draft.isSelected}
        onToggle={draft.toggle}
        onSetMany={draft.setMany}
        disabled={editor.saving}
        title={t('optimization.apps.catalogTitle')}
        description={t('optimization.apps.catalogBody')}
      />
      <ItemProgressTable
        kind="app"
        items={posture?.items}
        index={editor.index}
        isLoading={postureLoading}
        title={t('optimization.apps.progressTitle')}
        description={t('optimization.apps.progressBody')}
      />
    </div>
  )
}
