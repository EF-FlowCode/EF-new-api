import {
  CameraIcon,
  FileIcon,
  ImageIcon,
  ScreenShareIcon,
  type LucideIcon,
} from 'lucide-react'

type AttachmentAction = {
  action: string
  icon: LucideIcon
  label: string
}

type InputToolNotice = {
  description?: string
  title: string
}

// upload-photo / upload-file 打开图片选择框；其余仍是开发中提示
export const FILE_DIALOG_ATTACHMENT_ACTIONS: ReadonlySet<string> = new Set([
  'upload-file',
  'upload-photo',
])

export const ATTACHMENT_ACTIONS = [
  { action: 'upload-file', icon: FileIcon, label: 'Upload file' },
  { action: 'upload-photo', icon: ImageIcon, label: 'Upload photo' },
  {
    action: 'take-screenshot',
    icon: ScreenShareIcon,
    label: 'Take screenshot',
  },
  { action: 'take-photo', icon: CameraIcon, label: 'Take photo' },
] satisfies AttachmentAction[]

export function getAttachmentActionNotice(action: string): InputToolNotice {
  return {
    description: action,
    title: 'Feature in development',
  }
}

export function getSearchActionNotice(): InputToolNotice {
  return {
    title: 'Search feature in development',
  }
}
