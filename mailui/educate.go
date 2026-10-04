package mailui

import (
	"github.com/codemodify/uitoolkit/style"
	"github.com/codemodify/uitoolkit/widget"
	"github.com/codemodify/uitoolkit/widgets"
)

// Settings › Security › Educate: what keeps email safe and what does not,
// for someone who has never thought about it — in short sections, each a
// picture and a few plain sentences, and where comms-mail does its part.

// lesson is one section: its heading, its picture (nil for none), what it
// says, and what comms-mail does about it.
type lesson struct {
	title string
	fig   func() *figure
	says  []string
	here  string
}

func lessons() []lesson {
	you := figNode{pict: pictPerson, label: "You"}
	them := figNode{pict: pictPerson, label: "The other person"}
	yourServer := figNode{pict: pictServer, label: "Your mail provider"}
	theirServer := figNode{pict: pictServer, label: "Their mail provider"}
	return []lesson{
		{
			title: "Email is a postcard",
			fig: func() *figure {
				ys, ts := yourServer, theirServer
				ys.badge, ts.badge = pictEye, pictEye
				return newFigure([]figNode{you, ys, ts, them},
					figLink{label: "you send", pict: pictLetter},
					figLink{label: "passed on", pict: pictLetter},
					figLink{label: "delivered", pict: pictLetter})
			},
			says: []string{
				"An email does not go straight to the other person. It goes from you to your mail provider's computer (a server), from there to theirs, and then to them.",
				"Ordinary email is like a postcard: whoever handles it on the way could read it — your provider, theirs, and anyone who breaks into either. The eye marks who can read it.",
				"That is fine for most mail. For private things there are ways to seal it: see Signatures and Encryption below.",
			},
		},
		{
			title: "Locked on the road",
			fig: func() *figure {
				ys, ts := yourServer, theirServer
				ys.badge, ts.badge = pictEye, pictEye
				return newFigure([]figNode{you, ys, ts, them},
					figLink{label: "locked", pict: pictLock, tone: toneGood},
					figLink{label: "locked", pict: pictLock, tone: toneGood},
					figLink{label: "locked", pict: pictLock, tone: toneGood})
			},
			says: []string{
				"The trip between your computer and your mail provider is protected by a locked connection — TLS, the same lock as https:// in a web browser. Nobody on a café's Wi-Fi or at your internet company can read or change your mail on the way.",
				"But the lock covers only the road. At each provider the mail is unpacked again, as readable as before.",
			},
			here: "every account uses a locked connection, and never falls back to an open one by itself.",
		},
		{
			title: "Your password, and where it is kept",
			fig: func() *figure {
				return newFigure([]figNode{
					{pict: pictKey, label: "Your mail password"},
					{pict: pictLock, label: "A safe place", tone: toneGood},
					{pict: pictLetter, label: "comms-mail"},
					yourServer,
				},
					figLink{label: "kept in"},
					figLink{label: "handed over when needed"},
					figLink{label: "signs in", pict: pictLock, tone: toneGood})
			},
			says: []string{
				"To fetch and send your mail, comms-mail signs in to your provider each time it connects, so it has to keep your password somewhere. Where matters: whoever gets that password can read all your mail and send mail as you.",
				"System Keyring is your computer's own safe, opened when you log in. Secret Vault is your own vault program, which asks you before letting comms-mail in. Encrypted file is a file locked with a passphrase only you know. Plain file is readable by any program on your computer — avoid it.",
				"Signing in with Google or Microsoft gives comms-mail something like a valet key: it opens your mail and nothing else, and you can take it back from your account's security page without changing your password.",
			},
			here: "choose where in Security › Passwords.",
		},
		{
			title: "Who really sent it?",
			fig: func() *figure {
				return newFigure([]figNode{
					{pict: pictPerson, label: "A stranger", tone: toneBad},
					{pict: pictServer, label: "Your mail provider checks", badge: pictWarning},
					{pict: pictPerson, label: "You are warned", badge: pictWarning},
				},
					figLink{label: `"From: your bank"`, pict: pictLetter, tone: toneBad},
					figLink{label: "could not confirm", tone: toneBad})
			},
			says: []string{
				"The From line of an email is just text the sender typed. Anyone can put any name and address there, like a false return address on an envelope.",
				"Mail providers fight this with three checks. SPF: is this server allowed to send mail for that address? DKIM: a tamper-proof stamp the sender's provider puts on each email. DMARC: the sender's own rule for what to do when the first two fail. Your provider runs them and writes down the result in the email.",
			},
			here: "when your provider could not confirm who sent a message, a warning says so above it. Treat it like a phone call from an unknown number saying it is your bank.",
		},
		{
			title: "Look-alikes",
			fig: func() *figure {
				return newFigure([]figNode{
					{pict: pictLetter, label: "paypal.com", badge: pictCheck},
					{pict: pictLetter, label: "paypa1.com", badge: pictWarning, tone: toneBad},
				},
					figLink{label: "not the same", plain: true, tone: toneBad})
			},
			says: []string{
				"Tricksters register addresses that look almost right: paypa1.com with the number one, rnicrosoft.com with an r and an n, or a friendly name such as \"PayPal Service\" over an address somewhere else entirely.",
				"Before you click: does the address match, letter for letter? When in doubt, do not use the link in the email — go to the website yourself, or phone them.",
			},
			here: "a warning when a sender looks like someone you write to but is not, when the name shows a different address, and when replies would go somewhere else.",
		},
		{
			title: "Pictures that spy",
			fig: func() *figure {
				return newFigure([]figNode{
					{pict: pictPerson, label: "You open the email"},
					{pict: pictPicture, label: "A tiny picture on their server"},
					{pict: pictPerson, label: "The sender", badge: pictEye},
				},
					figLink{label: "fetched", pict: pictPicture},
					figLink{label: "you opened it, when, and from where", tone: toneBad})
			},
			says: []string{
				"Many emails carry tiny, invisible pictures kept on the sender's server. To show them, your mail program has to fetch them — and that tells the sender you opened the email, when, and roughly where you are.",
			},
			here: "pictures from the internet are not loaded unless you ask. Always, for a sender, holds only for mail your provider confirmed came from them; Security › Remote images lists whom you allowed.",
		},
		{
			title: "Signatures: a seal on the letter",
			fig: func() *figure {
				return newFigure([]figNode{
					{pict: pictPerson, label: "You", badge: pictKey},
					{pict: pictLetter, label: "Your email", badge: pictSeal},
					{pict: pictPerson, label: "The other person", badge: pictCheck},
				},
					figLink{label: "sealed with your private key", pict: pictPen},
					figLink{label: "checked with your public key", pict: pictKey, tone: toneGood})
			},
			says: []string{
				"A digital signature proves two things: the email really is from you, and nobody changed a single letter on the way.",
				"It works with a pair of keys. Your private key makes the seal, and only you have it. Your public key checks the seal, and you can give it to everybody. Without your private key, nobody can make a seal that checks.",
				"A signature hides nothing: everyone on the way can still read the email.",
			},
			here: "signed mail shows who signed it and whether that is confirmed, and what you write is signed whenever you have a key.",
		},
		{
			title: "Encryption: a locked box",
			fig: func() *figure {
				return newFigure([]figNode{
					{pict: pictPerson, label: "You", badge: pictPadlockOpen},
					{pict: pictServer, label: "The providers", badge: pictLock, tone: toneGood},
					{pict: pictPerson, label: "The other person", badge: pictKey},
				},
					figLink{label: "locked with their padlock", pict: pictLock, tone: toneGood},
					figLink{label: "opened with their key", pict: pictLock, tone: toneGood})
			},
			says: []string{
				"Encryption puts the email in a box only the other person can open. The providers carry the box but cannot look inside — not even yours.",
				"Think of padlocks. The other person hands out open padlocks — their public key — to anyone. You snap one shut on your box. Only their own key — their private key — opens it again; not even you can.",
				"So to send someone encrypted mail you need their public key first, and they need yours to answer. Still visible to the providers: who it is from, who it is to, and when.",
			},
			here: "Encrypt is in the Write window, and drafts of encrypted mail stay on this computer, never on the server.",
		},
		{
			title: "Two kinds: OpenPGP and S/MIME",
			fig: func() *figure {
				return newFigure([]figNode{
					{pict: pictPeople, label: "OpenPGP: you check the key, or people you trust did"},
					{pict: pictSeal, label: "S/MIME: an authority checked who you are"},
				},
					figLink{label: "same job, different trust", plain: true})
			},
			says: []string{
				"There are two ways to sign and encrypt email. They do the same job; they differ in how you know a public key really is that person's.",
				"OpenPGP: you check the key yourself — compare its fingerprint face to face or on the phone — or trust people who did. Like knowing a friend's handwriting.",
				"S/MIME: a certificate authority checks who you are and issues a certificate, the way a passport office does. Common at companies and in public services.",
				"They do not mix: you use the kind the other person uses.",
			},
			here: "both, read and written.",
		},
		{
			title: "Your private key is you",
			fig: func() *figure {
				return newFigure([]figNode{
					{pict: pictKey, label: "Your private key"},
					{pict: pictPerson, label: "Someone else", tone: toneBad},
					{pict: pictPeople, label: "Your contacts", badge: pictWarning},
				},
					figLink{label: "if it leaks", tone: toneBad},
					figLink{label: "reads your mail and signs as you", pict: pictLetter, tone: toneBad})
			},
			says: []string{
				"If someone copies your private key, they can read your encrypted mail and sign as you. If you lose it, encrypted mail sent to you can never be opened again — there is no reset button.",
				"So keep it somewhere locked, keep a backup somewhere safe, and if it ever leaks, make a new one and tell the people you write to.",
			},
			here: "your keys are in Security › Keys.",
		},
		{
			title: "Good habits",
			says: []string{
				"Use a password for your email that you use nowhere else — or sign in with Google or Microsoft — and turn on two-step sign-in at your provider.",
				"Never type your password after clicking a link in an email. Go to the website yourself.",
				"Do not open attachments you did not expect, even from people you know: ask them first.",
				"A warning in comms-mail means stop and check, not click anyway.",
				"Keep your computer and comms-mail up to date.",
				"For anything really private, encrypt it — or do not email it.",
			},
		},
	}
}

// educateSection is the Educate page: every lesson in turn.
func educateSection() widget.Component {
	col := widgets.NewColumn().WithGap(10)
	for i, l := range lessons() {
		if i > 0 {
			col.Add(widgets.NewSeparator())
		}
		col.Add(widgets.NewTitle(l.title))
		if l.fig != nil {
			col.Add(l.fig())
		}
		for _, s := range l.says {
			if l.fig == nil {
				col.Add(iconLine(style.IconCheck, s)) // a list of habits
				continue
			}
			col.Add(wrapLabel(s))
		}
		if l.here != "" {
			col.Add(iconLine(style.IconInfo, "In comms-mail: "+l.here))
		}
	}
	return widgets.NewScrollView(widgets.NewPad(4, col))
}
