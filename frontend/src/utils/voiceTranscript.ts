// Some speech servers wrap the transcript in a protocol prefix such as
// "language English<asr_text>Hello". Keep only what the user actually said.
const ASR_TEXT_PREFIX = /^\s*language\s*:?\s*[^<\s]+\s*<asr_text>\s*/i;

export function cleanVoiceTranscript(text: string): string {
  return text.replace(ASR_TEXT_PREFIX, '').trim();
}
