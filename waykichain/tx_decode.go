/*
 * Copyright 2018 The openwallet Authors
 * This file is part of the openwallet library.
 *
 * The openwallet library is free software: you can redistribute it and/or modify
 * it under the terms of the GNU Lesser General Public License as published by
 * the Free Software Foundation, either version 3 of the License, or
 * (at your option) any later version.
 *
 * The openwallet library is distributed in the hope that it will be useful,
 * but WITHOUT ANY WARRANTY; without even the implied warranty of
 * MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
 * GNU Lesser General Public License for more details.
 */

package waykichain

import (
	"encoding/hex"
	"errors"
	"fmt"
	"math/big"
	"sort"
	"strings"
	"time"

	"github.com/blocktree/go-owcdrivers/waykichainTransaction"
	"github.com/blocktree/openwallet/log"
	"github.com/blocktree/openwallet/openwallet"
)

type TransactionDecoder struct {
	openwallet.TransactionDecoderBase
	wm *WalletManager //钱包管理者
}

//NewTransactionDecoder 交易单解析器
func NewTransactionDecoder(wm *WalletManager) *TransactionDecoder {
	decoder := TransactionDecoder{}
	decoder.wm = wm
	return &decoder
}

//CreateRawTransaction 创建交易单
func (decoder *TransactionDecoder) CreateRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction) error {
	if rawTx.GetExtParam().Get("memo").String() != "" {
		return decoder.CreateWICCRegisterRawTransaction(wrapper, rawTx, rawTx.GetExtParam().Get("memo").String())
	}
	return decoder.CreateWICCRawTransaction(wrapper, rawTx)
}

//SignRawTransaction 签名交易单
func (decoder *TransactionDecoder) SignRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction) error {
	return decoder.SignWICCRawTransaction(wrapper, rawTx)
}

//VerifyRawTransaction 验证交易单，验证交易单并返回加入签名后的交易单
func (decoder *TransactionDecoder) VerifyRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction) error {
	return decoder.VerifyWICCRawTransaction(wrapper, rawTx)
}

func (decoder *TransactionDecoder) SubmitRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction) (*openwallet.Transaction, error) {
	if len(rawTx.RawHex) == 0 {
		return nil, fmt.Errorf("transaction hex is empty")
	}

	if !rawTx.IsCompleted {
		return nil, fmt.Errorf("transaction is not completed validation")
	}

	txid, err := decoder.wm.SendRawTransaction(rawTx.RawHex)
	if err != nil {
		fmt.Println("Tx to send: ", rawTx.RawHex)
		return nil, err
	}

	rawTx.TxID = txid
	rawTx.IsSubmit = true

	decimals := int32(8)

	tx := openwallet.Transaction{
		From:       rawTx.TxFrom,
		To:         rawTx.TxTo,
		Amount:     rawTx.TxAmount,
		Coin:       rawTx.Coin,
		TxID:       rawTx.TxID,
		Decimal:    decimals,
		AccountID:  rawTx.Account.AccountID,
		Fees:       rawTx.Fees,
		SubmitTime: time.Now().Unix(),
	}

	tx.WxID = openwallet.GenTransactionWxID(&tx)

	return &tx, nil
}

func (decoder *TransactionDecoder) CreateWICCRegisterRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction, memo string) error {
	memos := strings.Split(memo, ":")
	if len(memos) != 2 || memos[0] != "register" {
		return errors.New("Invalid memo to register!")
	}
	_, err := waykichainTransaction.GetProgramHashFromAddress(memos[1])
	if err != nil {
		return errors.New("Invalid address to be registered!")
	}

	address, err := wrapper.GetAddress(memos[1])
	if err != nil {
		return err
	}
	validHeight, err := decoder.wm.Client.getBlockHeight()
	if err != nil {
		return errors.New("Failed to get block height when create register transaction!")
	}

	rawTx.TxFrom = []string{memos[1]}
	rawTx.TxTo = []string{}
	rawTx.TxAmount = convertToAmount(uint64(decoder.wm.Config.RegisterFee))
	rawTx.Fees = "0"
	emptyTrans, hash, err := waykichainTransaction.CreateEmptyRawTransactionAndHash(address.PublicKey, "", 0, decoder.wm.Config.RegisterFee, int64(validHeight), waykichainTransaction.TxType_REGACCT)

	rawTx.RawHex = emptyTrans

	if rawTx.Signatures == nil {
		rawTx.Signatures = make(map[string][]*openwallet.KeySignature)
	}

	keySigs := make([]*openwallet.KeySignature, 0)

	addr, err := wrapper.GetAddress(memos[1])
	if err != nil {
		return err
	}
	signature := openwallet.KeySignature{
		EccType: decoder.wm.Config.CurveType,
		Nonce:   "",
		Address: addr,
		Message: hash,
	}

	keySigs = append(keySigs, &signature)

	rawTx.Signatures[rawTx.Account.AccountID] = keySigs

	rawTx.FeeRate = "0"

	rawTx.IsBuilt = true

	return nil
}
func (decoder *TransactionDecoder) CreateWICCRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction) error {

	addresses, err := wrapper.GetAddressList(0, -1, "AccountID", rawTx.Account.AccountID)

	if err != nil {
		return err
	}

	if len(addresses) == 0 {
		return openwallet.Errorf(openwallet.ErrAccountNotAddress, "[%s] have not addresses", rawTx.Account.AccountID)
	}

	addressesBalanceList := make([]AddrBalance, 0, len(addresses))

	for i, addr := range addresses {
		balance, err := decoder.wm.Client.getBalance(addr.Address)

		if err != nil {
			return err
		}

		balance.index = i
		addressesBalanceList = append(addressesBalanceList, *balance)
	}

	sort.Slice(addressesBalanceList, func(i int, j int) bool {
		return addressesBalanceList[i].Balance.Cmp(addressesBalanceList[j].Balance) >= 0
	})

	fee := uint64(0)
	if len(rawTx.FeeRate) > 0 {
		fee = convertFromAmount(rawTx.FeeRate)
	} else {
		fee = uint64(decoder.wm.Config.FixedFee)
	}

	var amountStr, to string
	for k, v := range rawTx.To {
		to = k
		amountStr = v
		break
	}
	// keySignList := make([]*openwallet.KeySignature, 1, 1)

	amount := big.NewInt(int64(convertFromAmount(amountStr)))

	minTransferAmount := big.NewInt(decoder.wm.Config.MinTransferAmount)
	if amount.Cmp(minTransferAmount) < 0 {
		return errors.New("Amount too small,the minimum amount of WICC transfer is 0.0001 WICC!")
	}

	amount = amount.Add(amount, big.NewInt(int64(fee)))

	from := ""
	available := ""
	count := big.NewInt(0)
	countList := []uint64{}
	fromUserID := ""
	for _, a := range addressesBalanceList {
		if a.Balance.Cmp(amount) < 0 {
			count.Add(count, a.Balance)
			if count.Cmp(amount) >= 0 {
				countList = append(countList, a.Balance.Sub(a.Balance, count.Sub(count, amount)).Uint64())
				decoder.wm.Log.Std.Notice("The WICC of the account is enough," +
					" but cannot be sent in just one transaction!")
				return err
			} else {
				countList = append(countList, a.Balance.Uint64())
			}
			continue
		} else if !a.Registered {
			regFee := big.NewInt(decoder.wm.Config.RegisterFee)
			regFee = regFee.Add(regFee, amount)
			if available == "" && regFee.Cmp(a.Balance) < 0 {
				available = a.Address
			}

			continue
		}
		from = a.Address
		fromUserID = a.UserID
	}

	if from == "" {
		if available == "" {
			return openwallet.Errorf(openwallet.ErrInsufficientBalanceOfAccount, "the balance: %s is not enough", amountStr)
		} else {
			return errors.New("Address [" + available + "] has enough WICC to send, but which is not registered. Please set memo to \"register:" + available + "\" to register the address!")
		}
	}

	rawTx.TxFrom = []string{from}
	rawTx.TxTo = []string{to}
	rawTx.TxAmount = amountStr
	rawTx.Fees = convertToAmount(fee)
	rawTx.FeeRate = convertToAmount(fee)
	validHeight, err := decoder.wm.Client.getBlockHeight()
	if err != nil {
		return errors.New("Failed to get block height when create transaction!")
	}

	emptyTrans, hash, err := waykichainTransaction.CreateEmptyRawTransactionAndHash(fromUserID, to, int64(convertFromAmount(amountStr)), int64(fee), int64(validHeight), waykichainTransaction.TxType_COMMON)
	if err != nil {
		return err
	}
	rawTx.RawHex = emptyTrans

	if rawTx.Signatures == nil {
		rawTx.Signatures = make(map[string][]*openwallet.KeySignature)
	}

	keySigs := make([]*openwallet.KeySignature, 0)

	addr, err := wrapper.GetAddress(from)
	if err != nil {
		return err
	}
	signature := openwallet.KeySignature{
		EccType: decoder.wm.Config.CurveType,
		Nonce:   "",
		Address: addr,
		Message: hash,
	}

	keySigs = append(keySigs, &signature)

	rawTx.Signatures[rawTx.Account.AccountID] = keySigs

	rawTx.FeeRate = big.NewInt(int64(fee)).String()

	rawTx.IsBuilt = true

	return nil
}

func (decoder *TransactionDecoder) SignWICCRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction) error {
	key, err := wrapper.HDKey()
	if err != nil {
		return nil
	}

	keySignatures := rawTx.Signatures[rawTx.Account.AccountID]

	if keySignatures != nil {
		for _, keySignature := range keySignatures {

			childKey, err := key.DerivedKeyWithPath(keySignature.Address.HDPath, keySignature.EccType)
			keyBytes, err := childKey.GetPrivateKeyBytes()
			if err != nil {
				return err
			}

			//签名交易
			///////交易单哈希签名
			signature, err := waykichainTransaction.SignRawTransaction(keySignature.Message, keyBytes)
			if err != nil {
				return fmt.Errorf("transaction hash sign failed, unexpected error: %v", err)
			} else {

				//for i, s := range sigPub {
				//	log.Info("第", i+1, "个签名结果")
				//	log.Info()
				//	log.Info("对应的公钥为")
				//	log.Info(hex.EncodeToString(s.Pubkey))
				//}

				// txHash.Normal.SigPub = *sigPub
			}

			keySignature.Signature = hex.EncodeToString(signature)
		}
	}

	log.Info("transaction hash sign success")

	rawTx.Signatures[rawTx.Account.AccountID] = keySignatures

	return nil
}

func (decoder *TransactionDecoder) VerifyWICCRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction) error {

	var (
		emptyTrans      = rawTx.RawHex
		signaturePubkey = waykichainTransaction.SigPub{}
	)

	for accountID, keySignatures := range rawTx.Signatures {
		log.Debug("accountID Signatures:", accountID)
		for _, keySignature := range keySignatures {

			signature, _ := hex.DecodeString(keySignature.Signature)
			pubkey, _ := hex.DecodeString(keySignature.Address.PublicKey)

			signaturePubkey = waykichainTransaction.SigPub{
				Signature: signature,
				PublicKey: pubkey,
			}

			log.Debug("Signature:", keySignature.Signature)
			log.Debug("PublicKey:", keySignature.Address.PublicKey)
		}
	}

	pass, signedTrans := waykichainTransaction.VerifyAndCombineRawTransaction(emptyTrans, signaturePubkey)

	if pass {
		log.Debug("transaction verify passed")
		rawTx.IsCompleted = true
		rawTx.RawHex = signedTrans
	} else {
		log.Debug("transaction verify failed")
		rawTx.IsCompleted = false
	}

	return nil
}

func (decoder *TransactionDecoder) GetRawTransactionFeeRate() (feeRate string, unit string, err error) {
	rate := uint64(decoder.wm.Config.FixedFee)
	return convertToAmount(rate), "TX", nil
}

//CreateSummaryRawTransaction 创建汇总交易，返回原始交易单数组
func (decoder *TransactionDecoder) CreateSummaryRawTransaction(wrapper openwallet.WalletDAI, sumRawTx *openwallet.SummaryRawTransaction) ([]*openwallet.RawTransaction, error) {
	if sumRawTx.Coin.IsContract {
		return nil, nil
	} else {
		return decoder.CreateSimpleSummaryRawTransaction(wrapper, sumRawTx)
	}
}

func (decoder *TransactionDecoder) CreateSimpleSummaryRawTransaction(wrapper openwallet.WalletDAI, sumRawTx *openwallet.SummaryRawTransaction) ([]*openwallet.RawTransaction, error) {

	var (
		rawTxArray      = make([]*openwallet.RawTransaction, 0)
		accountID       = sumRawTx.Account.AccountID
		minTransfer     = big.NewInt(int64(convertFromAmount(sumRawTx.MinTransfer)))
		retainedBalance = big.NewInt(int64(convertFromAmount(sumRawTx.RetainedBalance)))
	)

	if minTransfer.Cmp(retainedBalance) < 0 {
		return nil, fmt.Errorf("mini transfer amount must be greater than address retained balance")
	}

	//获取wallet
	addresses, err := wrapper.GetAddressList(sumRawTx.AddressStartIndex, sumRawTx.AddressLimit,
		"AccountID", sumRawTx.Account.AccountID)
	if err != nil {
		return nil, err
	}

	if len(addresses) == 0 {
		return nil, fmt.Errorf("[%s] have not addresses", accountID)
	}

	searchAddrs := make([]string, 0)
	for _, address := range addresses {
		searchAddrs = append(searchAddrs, address.Address)
	}

	addrBalanceArray, err := decoder.wm.Blockscanner.GetBalanceByAddress(searchAddrs...)
	if err != nil {
		return nil, err
	}

	for _, addrBalance := range addrBalanceArray {

		if decoder.wm.Client.isAddressRegistered(addrBalance.Address) {
			//检查余额是否超过最低转账
			addrBalance_BI := big.NewInt(int64(convertFromAmount(addrBalance.Balance)))

			if addrBalance_BI.Cmp(minTransfer) < 0 {
				continue
			}
			//计算汇总数量 = 余额 - 保留余额
			sumAmount_BI := new(big.Int)
			sumAmount_BI.Sub(addrBalance_BI, retainedBalance)

			//this.wm.Log.Debug("sumAmount:", sumAmount)
			//计算手续费
			feeInt := uint64(0)
			if len(sumRawTx.FeeRate) > 0 {
				feeInt = convertFromAmount(sumRawTx.FeeRate)
			} else {
				feeInt = uint64(decoder.wm.Config.FixedFee)
			}
			fee := big.NewInt(int64(feeInt))

			//减去手续费
			sumAmount_BI.Sub(sumAmount_BI, fee)
			if sumAmount_BI.Cmp(big.NewInt(0)) <= 0 {
				continue
			}

			sumAmount := convertToAmount(sumAmount_BI.Uint64())
			fees := convertToAmount(fee.Uint64())

			log.Debugf("balance: %v", addrBalance.Balance)
			log.Debugf("fees: %v", fees)
			log.Debugf("sumAmount: %v", sumAmount)

			//创建一笔交易单
			rawTx := &openwallet.RawTransaction{
				Coin:    sumRawTx.Coin,
				Account: sumRawTx.Account,
				To: map[string]string{
					sumRawTx.SummaryAddress: sumAmount,
				},
				Required: 1,
			}

			createErr := decoder.createRawTransaction(
				wrapper,
				rawTx,
				addrBalance)
			if createErr != nil {
				return nil, createErr
			}

			//创建成功，添加到队列
			rawTxArray = append(rawTxArray, rawTx)
		} else {
			//检查余额是否超过激活金额
			addrBalance_BI := big.NewInt(int64(convertFromAmount(addrBalance.Balance)))
			regFee := big.NewInt(decoder.wm.Config.RegisterFee)

			if addrBalance_BI.Cmp(regFee) < 0 {
				continue
			}

			decoder.wm.Log.Std.Notice("Address: %s need to be registered,which will take few minutes. you can do another summary flow after that!", addrBalance.Address)

			//创建一笔交易单
			rawTx := &openwallet.RawTransaction{
				Coin:     sumRawTx.Coin,
				Account:  sumRawTx.Account,
				To:       map[string]string{},
				Required: 1,
			}

			createErr := decoder.createRegRawTransaction(
				wrapper,
				rawTx,
				addrBalance)
			if createErr != nil {
				return nil, createErr
			}

			//创建成功，添加到队列
			rawTxArray = append(rawTxArray, rawTx)
		}

	}
	return rawTxArray, nil
}

func (decoder *TransactionDecoder) createRegRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction, addrBalance *openwallet.Balance) error {

	address, err := wrapper.GetAddress(addrBalance.Address)
	if err != nil {
		return err
	}

	validHeight, err := decoder.wm.Client.getBlockHeight()
	if err != nil {
		return errors.New("Failed to get block height when create register transaction!")
	}

	rawTx.TxFrom = []string{addrBalance.Address}
	rawTx.TxAmount = convertToAmount(uint64(decoder.wm.Config.RegisterFee))
	rawTx.Fees = "0"
	emptyTrans, hash, err := waykichainTransaction.CreateEmptyRawTransactionAndHash(address.PublicKey, "", 0, decoder.wm.Config.RegisterFee, int64(validHeight), waykichainTransaction.TxType_REGACCT)

	rawTx.RawHex = emptyTrans

	if rawTx.Signatures == nil {
		rawTx.Signatures = make(map[string][]*openwallet.KeySignature)
	}

	keySigs := make([]*openwallet.KeySignature, 0)

	addr, err := wrapper.GetAddress(addrBalance.Address)
	if err != nil {
		return err
	}
	signature := openwallet.KeySignature{
		EccType: decoder.wm.Config.CurveType,
		Nonce:   "",
		Address: addr,
		Message: hash,
	}

	keySigs = append(keySigs, &signature)

	rawTx.Signatures[rawTx.Account.AccountID] = keySigs

	rawTx.FeeRate = "0"

	rawTx.IsBuilt = true
	return nil
}
func (decoder *TransactionDecoder) createRawTransaction(wrapper openwallet.WalletDAI, rawTx *openwallet.RawTransaction, addrBalance *openwallet.Balance) error {

	fee := uint64(0)
	if len(rawTx.FeeRate) > 0 {
		fee = convertFromAmount(rawTx.FeeRate)
	} else {
		fee = uint64(decoder.wm.Config.FixedFee)
	}

	var amountStr, to string
	for k, v := range rawTx.To {
		to = k
		amountStr = v
		break
	}

	amount := big.NewInt(int64(convertFromAmount(amountStr)))
	amount = amount.Add(amount, big.NewInt(int64(fee)))
	from := addrBalance.Address
	fromAddr, err := wrapper.GetAddress(from)
	if err != nil {
		return err
	}

	rawTx.TxFrom = []string{from}
	rawTx.TxTo = []string{to}
	rawTx.TxAmount = amountStr
	rawTx.Fees = convertToAmount(fee)
	rawTx.FeeRate = convertToAmount(fee)

	validHeight, err := decoder.wm.Client.getBlockHeight()
	if err != nil {
		return errors.New("Failed to get block height when create transaction!")
	}
	fromUserID, err := decoder.wm.Client.getRegID(from)
	if err != nil {
		return err
	}

	emptyTrans, hash, err := waykichainTransaction.CreateEmptyRawTransactionAndHash(fromUserID, to, int64(convertFromAmount(amountStr)), int64(fee), int64(validHeight), waykichainTransaction.TxType_COMMON)

	if err != nil {
		return err
	}
	rawTx.RawHex = emptyTrans

	if rawTx.Signatures == nil {
		rawTx.Signatures = make(map[string][]*openwallet.KeySignature)
	}

	keySigs := make([]*openwallet.KeySignature, 0)

	signature := openwallet.KeySignature{
		EccType: decoder.wm.Config.CurveType,
		Nonce:   "",
		Address: fromAddr,
		Message: hash,
	}

	keySigs = append(keySigs, &signature)

	rawTx.Signatures[rawTx.Account.AccountID] = keySigs

	rawTx.FeeRate = big.NewInt(int64(fee)).String()

	rawTx.IsBuilt = true

	return nil
}

//CreateSummaryRawTransactionWithError 创建汇总交易，返回能原始交易单数组（包含带错误的原始交易单）
func (decoder *TransactionDecoder) CreateSummaryRawTransactionWithError(wrapper openwallet.WalletDAI, sumRawTx *openwallet.SummaryRawTransaction) ([]*openwallet.RawTransactionWithError, error) {
	raTxWithErr := make([]*openwallet.RawTransactionWithError, 0)
	rawTxs, err := decoder.CreateSummaryRawTransaction(wrapper, sumRawTx)
	if err != nil {
		return nil, err
	}
	for _, tx := range rawTxs {
		raTxWithErr = append(raTxWithErr, &openwallet.RawTransactionWithError{
			RawTx: tx,
			Error: nil,
		})
	}
	return raTxWithErr, nil
}
